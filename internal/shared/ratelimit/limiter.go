// Package ratelimit provides an in-process rate limiter with per-IP/per-user
// token buckets, per-endpoint profiles, and failed-auth lockouts.
//
// The limiter is designed to be shared across an HTTP service: construct one
// *Limiter in main, start its sweeper goroutine, and use (*Limiter).Middleware
// to wrap routes.
package ratelimit

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	"golang.org/x/time/rate"

	"github.com/Shisa-Fosho/services/internal/shared/observability"
)

// sampleSize controls the K in sample-K probabilistic LRU eviction. Matches
// Redis allkeys-lru tuning — higher values get closer to true LRU at O(k) cost.
const sampleSize = 5

// failureTTL bounds how long a single auth failure "counts" toward lockout.
// A sliding-window semantics: MaxFailures within this window triggers lockout.
// Failures older than this are evicted by sweep, preventing unbounded growth
// of the failures map under adversarial unique-IP floods.
const failureTTL = 15 * time.Minute

// Metric label values and reason codes. Kept as constants to prevent typos
// silently producing separate time series.
const (
	keyTypeIP        = "ip"
	keyTypeUser      = "user"
	evictReasonCap   = "cap"
	evictReasonSweep = "sweep"
)

// Profile describes the rate-limit behavior for a named class of endpoints.
type Profile struct {
	Name            string
	Rate            rate.Limit    // requests per second; use rate.Every for per-minute
	Burst           int           // max tokens available at once
	MaxFailures     int           // 0 disables lockout
	LockoutDuration time.Duration // 0 disables lockout
	MaxEntries      int           // hard cap on per-profile keyspace; 0 disables cap (tests only)
}

type entry struct {
	lim      *rate.Limiter
	lastSeen atomic.Int64 // unix-nano; written on every request, read by sweep
}

type lockoutState struct {
	until time.Time
}

type failureEntry struct {
	count       int
	lastFailure time.Time
}

// Limiter holds the shared state for rate limiting across a service.
type Limiter struct {
	profiles      map[string]*Profile          // read-only after NewLimiter
	ipEntries     map[string]map[string]*entry // profileName → ip → entry
	userEntries   map[string]map[string]*entry // profileName → address → entry
	failures      map[string]failureEntry      // ip → {count, lastFailure}; expired by sweep after failureTTL
	lockouts      map[string]lockoutState      // ip → lockout state
	lockoutsMax   int
	sweepBatch    int
	clock         func() time.Time
	userExtractor func(context.Context) string
	trustProxy    bool
	metrics       *observability.Metrics
	logger        *zap.Logger
	mu            sync.RWMutex
}

// NewLimiter validates the config and returns a ready-to-use Limiter.
// The sweeper is not started; call Start.
func NewLimiter(cfg Config) (*Limiter, error) {
	if cfg.Logger == nil {
		return nil, fmt.Errorf("ratelimit: Logger required")
	}
	if cfg.Metrics == nil {
		return nil, fmt.Errorf("ratelimit: Metrics required")
	}
	profiles := make(map[string]*Profile, len(cfg.Profiles))
	ipEntries := make(map[string]map[string]*entry, len(cfg.Profiles))
	userEntries := make(map[string]map[string]*entry, len(cfg.Profiles))
	for idx := range cfg.Profiles {
		profile := cfg.Profiles[idx]
		if profile.Name == "" {
			return nil, fmt.Errorf("ratelimit: profile with empty name")
		}
		if _, dup := profiles[profile.Name]; dup {
			return nil, fmt.Errorf("ratelimit: duplicate profile name %q", profile.Name)
		}
		if profile.Rate <= 0 || profile.Burst <= 0 {
			return nil, fmt.Errorf("ratelimit: profile %q has non-positive Rate/Burst", profile.Name)
		}
		profileCopy := profile
		profiles[profile.Name] = &profileCopy
		ipEntries[profile.Name] = make(map[string]*entry)
		userEntries[profile.Name] = make(map[string]*entry)
	}
	clock := cfg.Clock
	if clock == nil {
		clock = time.Now
	}
	batch := cfg.SweepBatchSize
	if batch <= 0 {
		batch = 500
	}
	lockMax := cfg.LockoutsMaxEntries
	if lockMax <= 0 {
		lockMax = 10_000
	}
	return &Limiter{
		profiles:      profiles,
		ipEntries:     ipEntries,
		userEntries:   userEntries,
		failures:      make(map[string]failureEntry),
		lockouts:      make(map[string]lockoutState),
		lockoutsMax:   lockMax,
		sweepBatch:    batch,
		clock:         clock,
		userExtractor: cfg.UserExtractor,
		trustProxy:    cfg.TrustProxyHeaders,
		metrics:       cfg.Metrics,
		logger:        cfg.Logger,
	}, nil
}

// Profile returns the named profile, or (nil, false) if unknown.
func (limiter *Limiter) Profile(name string) (*Profile, bool) {
	profile, ok := limiter.profiles[name]
	return profile, ok
}

// AllowIP consumes one token from the per-IP bucket for profile.
// Returns (true, 0) on allow, (false, retryAfter) on reject.
func (limiter *Limiter) AllowIP(profile *Profile, ip string) (bool, time.Duration) {
	return limiter.allow(limiter.ipEntries[profile.Name], profile, ip, keyTypeIP)
}

// AllowUser consumes one token from the per-user bucket for profile.
func (limiter *Limiter) AllowUser(profile *Profile, addr string) (bool, time.Duration) {
	return limiter.allow(limiter.userEntries[profile.Name], profile, addr, keyTypeUser)
}

func (limiter *Limiter) allow(entries map[string]*entry, profile *Profile, key, keyType string) (bool, time.Duration) {
	now := limiter.clock()
	limiter.mu.RLock()
	bucket, ok := entries[key]
	limiter.mu.RUnlock()
	if !ok {
		limiter.mu.Lock()
		if bucket, ok = entries[key]; !ok {
			if profile.MaxEntries > 0 && len(entries) >= profile.MaxEntries {
				limiter.evictOne(entries, profile.Name, keyType)
			}
			bucket = &entry{lim: rate.NewLimiter(profile.Rate, profile.Burst)}
			entries[key] = bucket
		}
		limiter.mu.Unlock()
	}
	bucket.lastSeen.Store(now.UnixNano())
	reservation := bucket.lim.ReserveN(now, 1)
	delay := reservation.DelayFrom(now)
	if delay == 0 {
		return true, 0
	}
	// Reject — give back the reservation so subsequent requests aren't charged
	// for it. CancelAt must receive the same clock we used for ReserveN;
	// res.Cancel() internally uses time.Now() and would corrupt state under
	// an injected clock.
	reservation.CancelAt(now)
	return false, delay
}

// IsLockedOut reports whether ip is currently locked out and the remaining time.
func (limiter *Limiter) IsLockedOut(ip string) (bool, time.Duration) {
	now := limiter.clock()
	limiter.mu.RLock()
	state, ok := limiter.lockouts[ip]
	limiter.mu.RUnlock()
	if !ok {
		return false, 0
	}
	if !now.Before(state.until) {
		return false, 0
	}
	return true, state.until.Sub(now)
}

// RecordAuthFailure bumps the failure counter for ip. When MaxFailures occur
// within failureTTL, a lockout of profile.LockoutDuration begins. No-op if
// profile disables lockout.
//
// Sliding-window semantics: a failure older than failureTTL resets the count,
// preventing ancient probes from contributing to a future lockout.
func (limiter *Limiter) RecordAuthFailure(ip string, profile *Profile) {
	if profile.MaxFailures <= 0 || profile.LockoutDuration <= 0 {
		return
	}
	now := limiter.clock()
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	if state, already := limiter.lockouts[ip]; already && now.Before(state.until) {
		return
	}

	failure := limiter.failures[ip]
	if now.Sub(failure.lastFailure) > failureTTL {
		failure.count = 0
	}
	failure.count++
	failure.lastFailure = now

	if failure.count >= profile.MaxFailures {
		if len(limiter.lockouts) >= limiter.lockoutsMax {
			limiter.evictOneLockout()
		}
		limiter.lockouts[ip] = lockoutState{until: now.Add(profile.LockoutDuration)}
		delete(limiter.failures, ip)
		limiter.metrics.RateLimitLockoutTotal.Inc()
		limiter.logger.Warn("rate limit lockout",
			zap.String("ip", ip),
			zap.String("profile", profile.Name),
			zap.Duration("lockout", profile.LockoutDuration),
		)
		return
	}
	limiter.failures[ip] = failure
}

// Start runs the sweeper until ctx is cancelled. Intended to be launched in
// a goroutine: `go limiter.Start(ctx, interval)`.
func (limiter *Limiter) Start(ctx context.Context, sweepInterval time.Duration) {
	if sweepInterval <= 0 {
		sweepInterval = time.Minute
	}
	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			limiter.sweep()
		}
	}
}

// staleCutoff is 2× the longest token-refill window across profiles.
// Entries untouched past this age are safe to evict — any bucket they
// backed has fully refilled, so recreating on next request is free.
func (limiter *Limiter) staleCutoff() time.Duration {
	var longest time.Duration
	for _, profile := range limiter.profiles {
		if profile.Rate <= 0 {
			continue
		}
		window := time.Duration(float64(time.Second) * float64(profile.Burst) / float64(profile.Rate))
		if window > longest {
			longest = window
		}
	}
	if longest == 0 {
		return 10 * time.Minute
	}
	return 2 * longest
}

type staleKey struct {
	profile string
	key     string
}

func (limiter *Limiter) sweep() {
	start := limiter.clock()
	cutoffNanos := start.Add(-limiter.staleCutoff()).UnixNano()

	var ipStale, userStale []staleKey
	var expiredLockouts, expiredFailures []string

	limiter.mu.RLock()
	for profileName, entries := range limiter.ipEntries {
		for key, bucket := range entries {
			if bucket.lastSeen.Load() < cutoffNanos {
				ipStale = append(ipStale, staleKey{profileName, key})
			}
		}
	}
	for profileName, entries := range limiter.userEntries {
		for key, bucket := range entries {
			if bucket.lastSeen.Load() < cutoffNanos {
				userStale = append(userStale, staleKey{profileName, key})
			}
		}
	}
	for key, state := range limiter.lockouts {
		if !start.Before(state.until) {
			expiredLockouts = append(expiredLockouts, key)
		}
	}
	failureCutoff := start.Add(-failureTTL)
	for key, failure := range limiter.failures {
		if failure.lastFailure.Before(failureCutoff) {
			expiredFailures = append(expiredFailures, key)
		}
	}
	limiter.mu.RUnlock()

	limiter.deleteStale(ipStale, limiter.ipEntries, keyTypeIP)
	limiter.deleteStale(userStale, limiter.userEntries, keyTypeUser)

	if len(expiredLockouts) > 0 || len(expiredFailures) > 0 {
		limiter.mu.Lock()
		now := limiter.clock()
		for _, key := range expiredLockouts {
			if state, ok := limiter.lockouts[key]; ok && !now.Before(state.until) {
				delete(limiter.lockouts, key)
			}
		}
		failureCutoff := now.Add(-failureTTL)
		for _, key := range expiredFailures {
			if failure, ok := limiter.failures[key]; ok && failure.lastFailure.Before(failureCutoff) {
				delete(limiter.failures, key)
			}
		}
		limiter.mu.Unlock()
	}

	limiter.metrics.RateLimitSweepDurationSeconds.Observe(time.Since(start).Seconds())
}

// deleteStale evicts the listed entries in batches of limiter.sweepBatch, releasing
// the write lock between batches so concurrent readers can interleave.
func (limiter *Limiter) deleteStale(keys []staleKey, target map[string]map[string]*entry, keyType string) {
	if len(keys) == 0 {
		return
	}
	for idx := 0; idx < len(keys); idx += limiter.sweepBatch {
		end := min(idx+limiter.sweepBatch, len(keys))
		batch := keys[idx:end]
		limiter.mu.Lock()
		for _, stale := range batch {
			delete(target[stale.profile], stale.key)
		}
		limiter.mu.Unlock()
		for _, stale := range batch {
			limiter.metrics.RateLimitEvictedTotal.WithLabelValues(stale.profile, keyType, evictReasonSweep).Inc()
		}
	}
}

// evictOne picks a victim via sample-K LRU: sample up to sampleSize random
// entries from entries (Go's map iteration is randomized, so ranging gives us that),
// and evict the one with the oldest lastSeen.
// Caller MUST hold limiter.mu as writer.
func (limiter *Limiter) evictOne(entries map[string]*entry, profile, keyType string) {
	var victim string
	var oldest int64
	sampled := 0
	for key, bucket := range entries {
		lastSeen := bucket.lastSeen.Load()
		if sampled == 0 || lastSeen < oldest {
			victim = key
			oldest = lastSeen
		}
		sampled++
		if sampled >= sampleSize {
			break
		}
	}
	if victim != "" {
		delete(entries, victim)
		limiter.metrics.RateLimitEvictedTotal.WithLabelValues(profile, keyType, evictReasonCap).Inc()
	}
}

// evictOneLockout picks the lockout with the soonest-expiring `until` from a
// sample — it was about to decay anyway. Random eviction would forgive an
// active attacker with a lot of lockout time left.
// Caller MUST hold limiter.mu as writer.
func (limiter *Limiter) evictOneLockout() {
	var victim string
	var soonest time.Time
	sampled := 0
	for key, state := range limiter.lockouts {
		if sampled == 0 || state.until.Before(soonest) {
			victim = key
			soonest = state.until
		}
		sampled++
		if sampled >= sampleSize {
			break
		}
	}
	if victim != "" {
		delete(limiter.lockouts, victim)
	}
}
