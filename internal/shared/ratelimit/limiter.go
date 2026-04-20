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
	for i := range cfg.Profiles {
		p := cfg.Profiles[i]
		if p.Name == "" {
			return nil, fmt.Errorf("ratelimit: profile with empty name")
		}
		if _, dup := profiles[p.Name]; dup {
			return nil, fmt.Errorf("ratelimit: duplicate profile name %q", p.Name)
		}
		if p.Rate <= 0 || p.Burst <= 0 {
			return nil, fmt.Errorf("ratelimit: profile %q has non-positive Rate/Burst", p.Name)
		}
		pCopy := p
		profiles[p.Name] = &pCopy
		ipEntries[p.Name] = make(map[string]*entry)
		userEntries[p.Name] = make(map[string]*entry)
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
func (l *Limiter) Profile(name string) (*Profile, bool) {
	p, ok := l.profiles[name]
	return p, ok
}

// AllowIP consumes one token from the per-IP bucket for p.
// Returns (true, 0) on allow, (false, retryAfter) on reject.
func (l *Limiter) AllowIP(p *Profile, ip string) (bool, time.Duration) {
	return l.allow(l.ipEntries[p.Name], p, ip, keyTypeIP)
}

// AllowUser consumes one token from the per-user bucket for p.
func (l *Limiter) AllowUser(p *Profile, addr string) (bool, time.Duration) {
	return l.allow(l.userEntries[p.Name], p, addr, keyTypeUser)
}

func (l *Limiter) allow(m map[string]*entry, p *Profile, key, keyType string) (bool, time.Duration) {
	now := l.clock()
	l.mu.RLock()
	e, ok := m[key]
	l.mu.RUnlock()
	if !ok {
		l.mu.Lock()
		if e, ok = m[key]; !ok {
			if p.MaxEntries > 0 && len(m) >= p.MaxEntries {
				l.evictOne(m, p.Name, keyType)
			}
			e = &entry{lim: rate.NewLimiter(p.Rate, p.Burst)}
			m[key] = e
		}
		l.mu.Unlock()
	}
	e.lastSeen.Store(now.UnixNano())
	res := e.lim.ReserveN(now, 1)
	delay := res.DelayFrom(now)
	if delay == 0 {
		return true, 0
	}
	// Reject — give back the reservation so subsequent requests aren't charged
	// for it. CancelAt must receive the same clock we used for ReserveN;
	// res.Cancel() internally uses time.Now() and would corrupt state under
	// an injected clock.
	res.CancelAt(now)
	return false, delay
}

// IsLockedOut reports whether ip is currently locked out and the remaining time.
func (l *Limiter) IsLockedOut(ip string) (bool, time.Duration) {
	now := l.clock()
	l.mu.RLock()
	ls, ok := l.lockouts[ip]
	l.mu.RUnlock()
	if !ok {
		return false, 0
	}
	if !now.Before(ls.until) {
		return false, 0
	}
	return true, ls.until.Sub(now)
}

// RecordAuthFailure bumps the failure counter for ip. When MaxFailures occur
// within failureTTL, a lockout of p.LockoutDuration begins. No-op if p
// disables lockout.
//
// Sliding-window semantics: a failure older than failureTTL resets the count,
// preventing ancient probes from contributing to a future lockout.
func (l *Limiter) RecordAuthFailure(ip string, p *Profile) {
	if p.MaxFailures <= 0 || p.LockoutDuration <= 0 {
		return
	}
	now := l.clock()
	l.mu.Lock()
	defer l.mu.Unlock()
	if ls, already := l.lockouts[ip]; already && now.Before(ls.until) {
		return
	}

	fe := l.failures[ip]
	if now.Sub(fe.lastFailure) > failureTTL {
		fe.count = 0
	}
	fe.count++
	fe.lastFailure = now

	if fe.count >= p.MaxFailures {
		if len(l.lockouts) >= l.lockoutsMax {
			l.evictOneLockout()
		}
		l.lockouts[ip] = lockoutState{until: now.Add(p.LockoutDuration)}
		delete(l.failures, ip)
		l.metrics.RateLimitLockoutTotal.Inc()
		l.logger.Warn("rate limit lockout",
			zap.String("ip", ip),
			zap.String("profile", p.Name),
			zap.Duration("lockout", p.LockoutDuration),
		)
		return
	}
	l.failures[ip] = fe
}

// Start runs the sweeper until ctx is cancelled. Intended to be launched in
// a goroutine: `go l.Start(ctx, interval)`.
func (l *Limiter) Start(ctx context.Context, sweepInterval time.Duration) {
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
			l.sweep()
		}
	}
}

// staleCutoff is 2× the longest token-refill window across profiles.
// Entries untouched past this age are safe to evict — any bucket they
// backed has fully refilled, so recreating on next request is free.
func (l *Limiter) staleCutoff() time.Duration {
	var longest time.Duration
	for _, p := range l.profiles {
		if p.Rate <= 0 {
			continue
		}
		w := time.Duration(float64(time.Second) * float64(p.Burst) / float64(p.Rate))
		if w > longest {
			longest = w
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

func (l *Limiter) sweep() {
	start := l.clock()
	cutoffNanos := start.Add(-l.staleCutoff()).UnixNano()

	var ipStale, userStale []staleKey
	var expiredLockouts, expiredFailures []string

	l.mu.RLock()
	for prof, m := range l.ipEntries {
		for k, e := range m {
			if e.lastSeen.Load() < cutoffNanos {
				ipStale = append(ipStale, staleKey{prof, k})
			}
		}
	}
	for prof, m := range l.userEntries {
		for k, e := range m {
			if e.lastSeen.Load() < cutoffNanos {
				userStale = append(userStale, staleKey{prof, k})
			}
		}
	}
	for k, ls := range l.lockouts {
		if !start.Before(ls.until) {
			expiredLockouts = append(expiredLockouts, k)
		}
	}
	failureCutoff := start.Add(-failureTTL)
	for k, fe := range l.failures {
		if fe.lastFailure.Before(failureCutoff) {
			expiredFailures = append(expiredFailures, k)
		}
	}
	l.mu.RUnlock()

	l.deleteStale(ipStale, l.ipEntries, keyTypeIP)
	l.deleteStale(userStale, l.userEntries, keyTypeUser)

	if len(expiredLockouts) > 0 || len(expiredFailures) > 0 {
		l.mu.Lock()
		now := l.clock()
		for _, k := range expiredLockouts {
			if ls, ok := l.lockouts[k]; ok && !now.Before(ls.until) {
				delete(l.lockouts, k)
			}
		}
		failureCutoff := now.Add(-failureTTL)
		for _, k := range expiredFailures {
			if fe, ok := l.failures[k]; ok && fe.lastFailure.Before(failureCutoff) {
				delete(l.failures, k)
			}
		}
		l.mu.Unlock()
	}

	l.metrics.RateLimitSweepDurationSeconds.Observe(time.Since(start).Seconds())
}

// deleteStale evicts the listed entries in batches of l.sweepBatch, releasing
// the write lock between batches so concurrent readers can interleave.
func (l *Limiter) deleteStale(keys []staleKey, target map[string]map[string]*entry, keyType string) {
	if len(keys) == 0 {
		return
	}
	for i := 0; i < len(keys); i += l.sweepBatch {
		end := min(i+l.sweepBatch, len(keys))
		batch := keys[i:end]
		l.mu.Lock()
		for _, sk := range batch {
			delete(target[sk.profile], sk.key)
		}
		l.mu.Unlock()
		for _, sk := range batch {
			l.metrics.RateLimitEvictedTotal.WithLabelValues(sk.profile, keyType, evictReasonSweep).Inc()
		}
	}
}

// evictOne picks a victim via sample-K LRU: sample up to sampleSize random
// entries from m (Go's map iteration is randomized, so ranging gives us that),
// and evict the one with the oldest lastSeen.
// Caller MUST hold l.mu as writer.
func (l *Limiter) evictOne(m map[string]*entry, profile, keyType string) {
	var victim string
	var oldest int64
	sampled := 0
	for k, e := range m {
		ls := e.lastSeen.Load()
		if sampled == 0 || ls < oldest {
			victim = k
			oldest = ls
		}
		sampled++
		if sampled >= sampleSize {
			break
		}
	}
	if victim != "" {
		delete(m, victim)
		l.metrics.RateLimitEvictedTotal.WithLabelValues(profile, keyType, evictReasonCap).Inc()
	}
}

// evictOneLockout picks the lockout with the soonest-expiring `until` from a
// sample — it was about to decay anyway. Random eviction would forgive an
// active attacker with a lot of lockout time left.
// Caller MUST hold l.mu as writer.
func (l *Limiter) evictOneLockout() {
	var victim string
	var soonest time.Time
	sampled := 0
	for k, ls := range l.lockouts {
		if sampled == 0 || ls.until.Before(soonest) {
			victim = k
			soonest = ls.until
		}
		sampled++
		if sampled >= sampleSize {
			break
		}
	}
	if victim != "" {
		delete(l.lockouts, victim)
	}
}
