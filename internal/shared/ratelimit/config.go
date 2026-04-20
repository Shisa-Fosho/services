package ratelimit

import (
	"context"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
	"golang.org/x/time/rate"

	"github.com/Shisa-Fosho/services/internal/shared/envutil"
	"github.com/Shisa-Fosho/services/internal/shared/observability"
)

// Config bundles the knobs NewLimiter needs.
type Config struct {
	Profiles           []Profile
	UserExtractor      func(context.Context) string
	TrustProxyHeaders  bool
	Clock              func() time.Time
	Metrics            *observability.Metrics
	Logger             *zap.Logger
	LockoutsMaxEntries int
	SweepBatchSize     int
}

// DefaultProfiles returns the built-in profiles used when no overrides exist.
//
// "auth" — strict, for credential-verify endpoints (signup, login, refresh,
// EIP-712 sig verify, HMAC sig verify). 20/min per IP, burst 5, 5 consecutive
// failures triggers a 15-minute lockout.
//
// "default" — loose IP-keyed backstop for the whole service. 300/min, burst 30.
func DefaultProfiles() []Profile {
	return []Profile{
		{
			Name:            "auth",
			Rate:            rate.Every(3 * time.Second),
			Burst:           5,
			MaxFailures:     5,
			LockoutDuration: 15 * time.Minute,
			MaxEntries:      10_000,
		},
		{
			Name:       "default",
			Rate:       rate.Every(200 * time.Millisecond),
			Burst:      30,
			MaxEntries: 50_000,
		},
	}
}

// LoadProfilesFromEnv returns DefaultProfiles with per-profile env overrides applied.
// For a profile named "X", env vars are RATELIMIT_X_RPM, RATELIMIT_X_BURST,
// RATELIMIT_X_MAX_FAILURES, RATELIMIT_X_LOCKOUT_SECONDS, RATELIMIT_X_MAX_ENTRIES.
func LoadProfilesFromEnv() []Profile {
	profiles := DefaultProfiles()
	for idx := range profiles {
		profile := &profiles[idx]
		prefix := "RATELIMIT_" + strings.ToUpper(profile.Name)
		if rpm := readPositiveInt(prefix + "_RPM"); rpm > 0 {
			profile.Rate = rate.Every(time.Minute / time.Duration(rpm))
		}
		if burst := readPositiveInt(prefix + "_BURST"); burst > 0 {
			profile.Burst = burst
		}
		if failures := readNonNegativeInt(prefix + "_MAX_FAILURES"); failures >= 0 {
			profile.MaxFailures = failures
		}
		if seconds := readNonNegativeInt(prefix + "_LOCKOUT_SECONDS"); seconds >= 0 {
			profile.LockoutDuration = time.Duration(seconds) * time.Second
		}
		if maxEntries := readPositiveInt(prefix + "_MAX_ENTRIES"); maxEntries > 0 {
			profile.MaxEntries = maxEntries
		}
	}
	return profiles
}

// SweepIntervalFromEnv reads RATELIMIT_SWEEP_INTERVAL_SECONDS (default 60).
func SweepIntervalFromEnv() time.Duration {
	if seconds := readPositiveInt("RATELIMIT_SWEEP_INTERVAL_SECONDS"); seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	return 60 * time.Second
}

// SweepBatchSizeFromEnv reads RATELIMIT_SWEEP_BATCH_SIZE (default 500).
func SweepBatchSizeFromEnv() int {
	if size := readPositiveInt("RATELIMIT_SWEEP_BATCH_SIZE"); size > 0 {
		return size
	}
	return 500
}

// LockoutsMaxFromEnv reads RATELIMIT_LOCKOUTS_MAX_ENTRIES (default 10,000).
func LockoutsMaxFromEnv() int {
	if maxEntries := readPositiveInt("RATELIMIT_LOCKOUTS_MAX_ENTRIES"); maxEntries > 0 {
		return maxEntries
	}
	return 10_000
}

// TrustProxyFromEnv reads RATELIMIT_TRUST_PROXY_HEADERS (default false).
func TrustProxyFromEnv() bool {
	return envutil.Get("RATELIMIT_TRUST_PROXY_HEADERS", "false") == "true"
}

func readPositiveInt(key string) int {
	raw := envutil.Get(key, "")
	if raw == "" {
		return 0
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 {
		return 0
	}
	return parsed
}

func readNonNegativeInt(key string) int {
	raw := envutil.Get(key, "")
	if raw == "" {
		return -1
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < 0 {
		return -1
	}
	return parsed
}
