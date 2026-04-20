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
	val, _ := ctx.Value(userCtxKey{}).(string)
	return val
}

func newMwLimiter(t *testing.T, profile Profile, extractor func(context.Context) string, trustProxy bool) (*Limiter, *fakeClock) {
	t.Helper()
	clk := &fakeClock{now: time.Unix(1_700_000_000, 0)}
	lim, err := NewLimiter(Config{
		Profiles:          []Profile{profile},
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
	handler := lim.Middleware("default", KeyByIP)(okHandler())
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "1.2.3.4:5555"
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
}

func TestMiddleware_Returns429WithRetryAfter(t *testing.T) {
	t.Parallel()
	lim, _ := newMwLimiter(t, Profile{Name: "default", Rate: rate.Every(time.Second), Burst: 1}, nil, false)
	handler := lim.Middleware("default", KeyByIP)(okHandler())

	// First request consumes the burst.
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "1.2.3.4:5555"
	handler.ServeHTTP(httptest.NewRecorder(), request)

	// Second request should be rejected.
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", recorder.Code)
	}
	if retryAfter := recorder.Header().Get("Retry-After"); retryAfter == "" {
		t.Fatal("expected Retry-After header")
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if body["error"] == "" {
		t.Fatalf("expected error envelope, got %v", body)
	}
}

func TestMiddleware_LockoutShortCircuits(t *testing.T) {
	t.Parallel()
	profile := Profile{
		Name:            "auth",
		Rate:            rate.Every(time.Second),
		Burst:           100, // generous so rate limit doesn't fire
		MaxFailures:     2,
		LockoutDuration: time.Hour,
	}
	lim, _ := newMwLimiter(t, profile, nil, false)
	prof, _ := lim.Profile("auth")
	lim.RecordAuthFailure("1.2.3.4", prof)
	lim.RecordAuthFailure("1.2.3.4", prof)

	called := false
	handler := lim.Middleware("auth", KeyByIP)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	request.RemoteAddr = "1.2.3.4:5555"
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", recorder.Code)
	}
	if called {
		t.Fatal("handler should not be invoked when locked out")
	}
}

func TestMiddleware_KeyByUserFallsBackToIP(t *testing.T) {
	t.Parallel()
	profile := Profile{Name: "default", Rate: rate.Every(time.Second), Burst: 1}
	lim, _ := newMwLimiter(t, profile, userExtractor, false)
	handler := lim.Middleware("default", KeyByUser)(okHandler())

	// No user in context — should key on IP.
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "1.2.3.4:5555"
	handler.ServeHTTP(httptest.NewRecorder(), request)

	// Second request from same IP, still no user — should 429.
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 (IP-keyed)", recorder.Code)
	}

	// Different user on same IP — different bucket, should pass.
	userRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	userRequest.RemoteAddr = "1.2.3.4:5555"
	userRequest = userRequest.WithContext(context.WithValue(context.Background(), userCtxKey{}, "alice"))
	userRecorder := httptest.NewRecorder()
	handler.ServeHTTP(userRecorder, userRequest)
	if userRecorder.Code != http.StatusOK {
		t.Fatalf("user-keyed status = %d, want 200", userRecorder.Code)
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
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request.RemoteAddr = testCase.remote
			if testCase.xff != "" {
				request.Header.Set("X-Forwarded-For", testCase.xff)
			}
			got := ClientIP(request, testCase.trust)
			if got != testCase.want {
				t.Errorf("clientIP = %q, want %q", got, testCase.want)
			}
		})
	}
}
