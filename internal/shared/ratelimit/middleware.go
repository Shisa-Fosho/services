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
func (limiter *Limiter) Middleware(profileName string, keyBy KeyStrategy) func(http.Handler) http.Handler {
	profile, ok := limiter.profiles[profileName]
	if !ok {
		panic(fmt.Sprintf("ratelimit: unknown profile %q", profileName))
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := ClientIP(r, limiter.trustProxy)
			if profile.MaxFailures > 0 {
				if locked, remaining := limiter.IsLockedOut(ip); locked {
					limiter.reject(w, r, profile, keyTypeIP, remaining)
					return
				}
			}
			allowed, retryAfter, keyType := limiter.applyProfile(r, profile, keyBy, ip)
			if !allowed {
				limiter.reject(w, r, profile, keyType, retryAfter)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (limiter *Limiter) applyProfile(r *http.Request, profile *Profile, keyBy KeyStrategy, ip string) (bool, time.Duration, string) {
	if keyBy == KeyByUser {
		if user := limiter.extractUser(r); user != "" {
			ok, retryAfter := limiter.AllowUser(profile, user)
			return ok, retryAfter, keyTypeUser
		}
	}
	ok, retryAfter := limiter.AllowIP(profile, ip)
	return ok, retryAfter, keyTypeIP
}

func (limiter *Limiter) extractUser(r *http.Request) string {
	if limiter.userExtractor == nil {
		return ""
	}
	return limiter.userExtractor(r.Context())
}

func (limiter *Limiter) reject(w http.ResponseWriter, r *http.Request, profile *Profile, keyType string, retryAfter time.Duration) {
	seconds := int(math.Ceil(retryAfter.Seconds()))
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	limiter.metrics.RateLimitRejectedTotal.WithLabelValues(profile.Name, keyType).Inc()
	limiter.logger.Debug("rate limit rejected",
		zap.String("request_id", observability.RequestIDFrom(r.Context())),
		zap.String("profile", profile.Name),
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
func ClientIP(request *http.Request, trustProxy bool) string {
	if trustProxy {
		if xff := request.Header.Get("X-Forwarded-For"); xff != "" {
			if idx := strings.IndexByte(xff, ','); idx >= 0 {
				return strings.TrimSpace(xff[:idx])
			}
			return strings.TrimSpace(xff)
		}
		if realIP := request.Header.Get("X-Real-IP"); realIP != "" {
			return strings.TrimSpace(realIP)
		}
	}
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		return request.RemoteAddr
	}
	return host
}
