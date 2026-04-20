package ratelimit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"
	"golang.org/x/time/rate"

	"github.com/Shisa-Fosho/services/internal/shared/observability"
)

type userCtxKey struct{}

func userExtractor(ctx context.Context) string {
	v, _ := ctx.Value(userCtxKey{}).(string)
	return v
}

func newMwLimiter(t *testing.T, p Profile, extractor func(context.Context) string, trustProxy bool) (*Limiter, *fakeClock) {
	t.Helper()
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	lim, err := NewLimiter(Config{
		Profiles:          []Profile{p},
		Clock:             clk.Now,
		UserExtractor:     extractor,
		TrustProxyHeaders: trustProxy,
		Metrics:           observability.NewUnregisteredMetrics("test"),
		Logger:            zaptest.NewLogger(t),
	})
	if err != nil {
		t.Fatalf("NewLimiter: %v", err)
	}
	return lim, clk
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
}

func TestMiddleware_HappyPath(t *testing.T) {
	t.Parallel()
	lim, _ := newMwLimiter(t, Profile{Name: "default", Rate: rate.Every(time.Second), Burst: 3}, nil, false)
	h := lim.Middleware("default", KeyByIP)(okHandler())
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "1.2.3.4:5555"
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

func TestMiddleware_Returns429WithRetryAfter(t *testing.T) {
	t.Parallel()
	lim, _ := newMwLimiter(t, Profile{Name: "default", Rate: rate.Every(time.Second), Burst: 1}, nil, false)
	h := lim.Middleware("default", KeyByIP)(okHandler())

	// First request consumes the burst.
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "1.2.3.4:5555"
	h.ServeHTTP(httptest.NewRecorder(), r)

	// Second request should be rejected.
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", w.Code)
	}
	if ra := w.Header().Get("Retry-After"); ra == "" {
		t.Fatal("expected Retry-After header")
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if body["error"] == "" {
		t.Fatalf("expected error envelope, got %v", body)
	}
}

func TestMiddleware_LockoutShortCircuits(t *testing.T) {
	t.Parallel()
	p := Profile{
		Name:            "auth",
		Rate:            rate.Every(time.Second),
		Burst:           100, // generous so rate limit doesn't fire
		MaxFailures:     2,
		LockoutDuration: time.Hour,
	}
	lim, _ := newMwLimiter(t, p, nil, false)
	prof, _ := lim.Profile("auth")
	lim.RecordAuthFailure("1.2.3.4", prof)
	lim.RecordAuthFailure("1.2.3.4", prof)

	called := false
	h := lim.Middleware("auth", KeyByIP)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	r.RemoteAddr = "1.2.3.4:5555"
	h.ServeHTTP(w, r)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", w.Code)
	}
	if called {
		t.Fatal("handler should not be invoked when locked out")
	}
}

func TestMiddleware_KeyByUserFallsBackToIP(t *testing.T) {
	t.Parallel()
	p := Profile{Name: "default", Rate: rate.Every(time.Second), Burst: 1}
	lim, _ := newMwLimiter(t, p, userExtractor, false)
	h := lim.Middleware("default", KeyByUser)(okHandler())

	// No user in context — should key on IP.
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "1.2.3.4:5555"
	h.ServeHTTP(httptest.NewRecorder(), r)

	// Second request from same IP, still no user — should 429.
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 (IP-keyed)", w.Code)
	}

	// Different user on same IP — different bucket, should pass.
	r2 := httptest.NewRequest(http.MethodGet, "/", nil)
	r2.RemoteAddr = "1.2.3.4:5555"
	r2 = r2.WithContext(context.WithValue(context.Background(), userCtxKey{}, "alice"))
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, r2)
	if w2.Code != http.StatusOK {
		t.Fatalf("user-keyed status = %d, want 200", w2.Code)
	}
}

func TestMiddleware_PanicsOnUnknownProfile(t *testing.T) {
	t.Parallel()
	lim, _ := newMwLimiter(t, Profile{Name: "default", Rate: rate.Every(time.Second), Burst: 1}, nil, false)
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on unknown profile name")
		}
	}()
	_ = lim.Middleware("nonexistent", KeyByIP)
}

func TestClientIPExported_ProxyHeaderRespectsTrust(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		trust  bool
		xff    string
		remote string
		want   string
	}{
		{"trust honors XFF leftmost", true, "10.0.0.1, 10.0.0.2", "192.0.2.1:1234", "10.0.0.1"},
		{"no trust ignores XFF", false, "10.0.0.1", "192.0.2.1:1234", "192.0.2.1"},
		{"remoteaddr fallback when XFF empty", true, "", "192.0.2.1:1234", "192.0.2.1"},
		{"bare remoteaddr without port", false, "", "192.0.2.1", "192.0.2.1"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = tc.remote
			if tc.xff != "" {
				r.Header.Set("X-Forwarded-For", tc.xff)
			}
			got := ClientIP(r, tc.trust)
			if got != tc.want {
				t.Errorf("clientIP = %q, want %q", got, tc.want)
			}
		})
	}
}
