package ratelimit

import (
	"fmt"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/Shisa-Fosho/services/internal/shared/httputil"
	"github.com/Shisa-Fosho/services/internal/shared/observability"
)

// KeyStrategy controls how the middleware keys the bucket.
type KeyStrategy int

const (
	// KeyByIP always keys on the client IP (RemoteAddr or leftmost XFF if
	// proxy headers are trusted).
	KeyByIP KeyStrategy = iota
	// KeyByUser keys on the user address from context when present, otherwise
	// falls back to IP. Use for routes that accept both authenticated and
	// unauthenticated traffic, or for routes behind an auth middleware where
	// a missing user implies an anonymous request that should still be bounded.
	KeyByUser
)

// Middleware returns an HTTP middleware enforcing the named profile.
// Panics at registration time if profileName is unknown — this is a
// programmer error and should surface immediately.
func (l *Limiter) Middleware(profileName string, keyBy KeyStrategy) func(http.Handler) http.Handler {
	p, ok := l.profiles[profileName]
	if !ok {
		panic(fmt.Sprintf("ratelimit: unknown profile %q", profileName))
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := ClientIP(r, l.trustProxy)
			if p.MaxFailures > 0 {
				if locked, remaining := l.IsLockedOut(ip); locked {
					l.reject(w, r, p, keyTypeIP, remaining)
					return
				}
			}
			allowed, retryAfter, keyType := l.applyProfile(r, p, keyBy, ip)
			if !allowed {
				l.reject(w, r, p, keyType, retryAfter)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (l *Limiter) applyProfile(r *http.Request, p *Profile, keyBy KeyStrategy, ip string) (bool, time.Duration, string) {
	if keyBy == KeyByUser {
		if user := l.extractUser(r); user != "" {
			ok, ra := l.AllowUser(p, user)
			return ok, ra, keyTypeUser
		}
	}
	ok, ra := l.AllowIP(p, ip)
	return ok, ra, keyTypeIP
}

func (l *Limiter) extractUser(r *http.Request) string {
	if l.userExtractor == nil {
		return ""
	}
	return l.userExtractor(r.Context())
}

func (l *Limiter) reject(w http.ResponseWriter, r *http.Request, p *Profile, keyType string, retryAfter time.Duration) {
	seconds := int(math.Ceil(retryAfter.Seconds()))
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	l.metrics.RateLimitRejectedTotal.WithLabelValues(p.Name, keyType).Inc()
	l.logger.Debug("rate limit rejected",
		zap.String("request_id", observability.RequestIDFrom(r.Context())),
		zap.String("profile", p.Name),
		zap.String("key_type", keyType),
		zap.Int("retry_after_seconds", seconds),
	)
	httputil.ErrorResponse(w, http.StatusTooManyRequests, "too many requests")
}

// ClientIP extracts the client IP. When trustProxy is true, the leftmost
// X-Forwarded-For entry is honored (then X-Real-IP); otherwise RemoteAddr
// is used. Trusting proxy headers when none is present is a spoof vector,
// which is why the default is false.
//
// Exported so services can key auth-failure callbacks on the same IP the
// middleware used.
func ClientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if i := strings.IndexByte(xff, ','); i >= 0 {
				return strings.TrimSpace(xff[:i])
			}
			return strings.TrimSpace(xff)
		}
		if xr := r.Header.Get("X-Real-IP"); xr != "" {
			return strings.TrimSpace(xr)
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
