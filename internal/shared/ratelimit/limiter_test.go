package ratelimit

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"
	"golang.org/x/time/rate"

	"github.com/Shisa-Fosho/services/internal/shared/observability"
)

// fakeClock is a test clock whose Now() returns whatever t points at.
type fakeClock struct{ t time.Time }

func (f *fakeClock) Now() time.Time          { return f.t }
func (f *fakeClock) Advance(d time.Duration) { f.t = f.t.Add(d) }

func newTestLimiter(t *testing.T, profiles []Profile) (*Limiter, *fakeClock, *observability.Metrics) {
	t.Helper()
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	m := observability.NewUnregisteredMetrics("test")
	lim, err := NewLimiter(Config{
		Profiles:       profiles,
		Clock:          clk.Now,
		Metrics:        m,
		Logger:         zaptest.NewLogger(t),
		SweepBatchSize: 2,
	})
	if err != nil {
		t.Fatalf("NewLimiter: %v", err)
	}
	return lim, clk, m
}

func TestAllowIP_WithinBurstPasses(t *testing.T) {
	t.Parallel()
	p := Profile{Name: "default", Rate: rate.Every(time.Second), Burst: 3}
	lim, _, _ := newTestLimiter(t, []Profile{p})
	prof, _ := lim.Profile("default")
	for i := 0; i < 3; i++ {
		ok, _ := lim.AllowIP(prof, "1.2.3.4")
		if !ok {
			t.Fatalf("request %d: expected allow within burst", i+1)
		}
	}
}

func TestAllowIP_OverBudgetRejects(t *testing.T) {
	t.Parallel()
	p := Profile{Name: "default", Rate: rate.Every(time.Second), Burst: 2}
	lim, _, _ := newTestLimiter(t, []Profile{p})
	prof, _ := lim.Profile("default")
	lim.AllowIP(prof, "1.2.3.4")
	lim.AllowIP(prof, "1.2.3.4")
	ok, retry := lim.AllowIP(prof, "1.2.3.4")
	if ok {
		t.Fatal("expected reject on 3rd request")
	}
	if retry <= 0 || retry > time.Second {
		t.Fatalf("retry-after = %v, want (0, 1s]", retry)
	}
}

func TestAllowIP_TokenRefillsOverTime(t *testing.T) {
	t.Parallel()
	p := Profile{Name: "default", Rate: rate.Every(100 * time.Millisecond), Burst: 1}
	lim, clk, _ := newTestLimiter(t, []Profile{p})
	prof, _ := lim.Profile("default")
	if ok, _ := lim.AllowIP(prof, "1.2.3.4"); !ok {
		t.Fatal("first request should pass")
	}
	if ok, _ := lim.AllowIP(prof, "1.2.3.4"); ok {
		t.Fatal("second immediate request should reject")
	}
	clk.Advance(150 * time.Millisecond)
	if ok, _ := lim.AllowIP(prof, "1.2.3.4"); !ok {
		t.Fatal("after refill, request should pass")
	}
}

func TestAllowIP_DifferentKeysIndependent(t *testing.T) {
	t.Parallel()
	p := Profile{Name: "default", Rate: rate.Every(time.Second), Burst: 1}
	lim, _, _ := newTestLimiter(t, []Profile{p})
	prof, _ := lim.Profile("default")
	lim.AllowIP(prof, "1.1.1.1")
	if ok, _ := lim.AllowIP(prof, "2.2.2.2"); !ok {
		t.Fatal("second IP should have its own budget")
	}
}

func TestRecordAuthFailure_LocksAfterN(t *testing.T) {
	t.Parallel()
	p := Profile{
		Name:            "auth",
		Rate:            rate.Every(time.Second),
		Burst:           10,
		MaxFailures:     3,
		LockoutDuration: 15 * time.Minute,
	}
	lim, _, _ := newTestLimiter(t, []Profile{p})
	prof, _ := lim.Profile("auth")

	if locked, _ := lim.IsLockedOut("1.2.3.4"); locked {
		t.Fatal("not locked before any failures")
	}
	lim.RecordAuthFailure("1.2.3.4", prof)
	lim.RecordAuthFailure("1.2.3.4", prof)
	if locked, _ := lim.IsLockedOut("1.2.3.4"); locked {
		t.Fatal("still not locked after 2 failures (threshold 3)")
	}
	lim.RecordAuthFailure("1.2.3.4", prof)
	locked, remaining := lim.IsLockedOut("1.2.3.4")
	if !locked {
		t.Fatal("should be locked after 3rd failure")
	}
	if remaining <= 0 || remaining > 15*time.Minute {
		t.Fatalf("remaining = %v, want (0, 15m]", remaining)
	}
}

func TestRecordAuthFailure_LockoutExpires(t *testing.T) {
	t.Parallel()
	p := Profile{
		Name:            "auth",
		Rate:            rate.Every(time.Second),
		Burst:           10,
		MaxFailures:     1,
		LockoutDuration: time.Minute,
	}
	lim, clk, _ := newTestLimiter(t, []Profile{p})
	prof, _ := lim.Profile("auth")
	lim.RecordAuthFailure("1.2.3.4", prof)
	if locked, _ := lim.IsLockedOut("1.2.3.4"); !locked {
		t.Fatal("should be locked immediately")
	}
	clk.Advance(61 * time.Second)
	if locked, _ := lim.IsLockedOut("1.2.3.4"); locked {
		t.Fatal("should be unlocked after duration elapsed")
	}
}

func TestRecordAuthFailure_NoLockoutForZeroConfig(t *testing.T) {
	t.Parallel()
	p := Profile{Name: "default", Rate: rate.Every(time.Second), Burst: 10} // MaxFailures=0
	lim, _, _ := newTestLimiter(t, []Profile{p})
	prof, _ := lim.Profile("default")
	for i := 0; i < 100; i++ {
		lim.RecordAuthFailure("1.2.3.4", prof)
	}
	if locked, _ := lim.IsLockedOut("1.2.3.4"); locked {
		t.Fatal("profile with MaxFailures=0 should never lock out")
	}
}

func TestAllowIP_CapEvictsOldestFromSample(t *testing.T) {
	t.Parallel()
	p := Profile{Name: "default", Rate: rate.Every(time.Second), Burst: 1, MaxEntries: 3}
	lim, clk, m := newTestLimiter(t, []Profile{p})
	prof, _ := lim.Profile("default")
	// Fill to cap, each with a distinct lastSeen.
	lim.AllowIP(prof, "a")
	clk.Advance(time.Second)
	lim.AllowIP(prof, "b")
	clk.Advance(time.Second)
	lim.AllowIP(prof, "c")
	clk.Advance(time.Second)
	// 4th insertion triggers eviction.
	lim.AllowIP(prof, "d")
	if got := testCounterValue(t, m.RateLimitEvictedTotal.WithLabelValues("default", "ip", "cap")); got != 1 {
		t.Fatalf("cap eviction counter = %v, want 1", got)
	}
	if got := len(lim.ipEntries["default"]); got != 3 {
		t.Fatalf("map size after eviction = %d, want 3", got)
	}
}

func TestSweep_EvictsStaleEntries(t *testing.T) {
	t.Parallel()
	p := Profile{Name: "default", Rate: rate.Every(time.Second), Burst: 5}
	lim, clk, m := newTestLimiter(t, []Profile{p})
	prof, _ := lim.Profile("default")
	lim.AllowIP(prof, "stale")
	// Advance well past 2× refill window (5s × 2 = 10s).
	clk.Advance(30 * time.Second)
	lim.AllowIP(prof, "fresh")
	lim.sweep()
	if _, ok := lim.ipEntries["default"]["stale"]; ok {
		t.Fatal("stale key should have been evicted")
	}
	if _, ok := lim.ipEntries["default"]["fresh"]; !ok {
		t.Fatal("fresh key should survive")
	}
	if got := testCounterValue(t, m.RateLimitEvictedTotal.WithLabelValues("default", "ip", "sweep")); got != 1 {
		t.Fatalf("sweep eviction counter = %v, want 1", got)
	}
}

func TestSweep_ExpiredLockoutsRemoved(t *testing.T) {
	t.Parallel()
	p := Profile{
		Name:            "auth",
		Rate:            rate.Every(time.Second),
		Burst:           5,
		MaxFailures:     1,
		LockoutDuration: time.Minute,
	}
	lim, clk, _ := newTestLimiter(t, []Profile{p})
	prof, _ := lim.Profile("auth")
	lim.RecordAuthFailure("1.1.1.1", prof)
	clk.Advance(2 * time.Minute)
	lim.sweep()
	if _, ok := lim.lockouts["1.1.1.1"]; ok {
		t.Fatal("expired lockout should be swept")
	}
}

func TestSweep_ExpiresFailuresBelowThreshold(t *testing.T) {
	t.Parallel()
	p := Profile{
		Name:            "auth",
		Rate:            rate.Every(time.Second),
		Burst:           5,
		MaxFailures:     5,
		LockoutDuration: time.Minute,
	}
	lim, clk, _ := newTestLimiter(t, []Profile{p})
	prof, _ := lim.Profile("auth")
	lim.RecordAuthFailure("1.1.1.1", prof)
	lim.RecordAuthFailure("1.1.1.1", prof)
	if _, ok := lim.failures["1.1.1.1"]; !ok {
		t.Fatal("failure entry should exist after 2 failures below threshold")
	}
	clk.Advance(failureTTL + time.Minute)
	lim.sweep()
	if _, ok := lim.failures["1.1.1.1"]; ok {
		t.Fatal("failure entry older than failureTTL should be swept")
	}
}

func TestRecordAuthFailure_StaleCountResets(t *testing.T) {
	t.Parallel()
	p := Profile{
		Name:            "auth",
		Rate:            rate.Every(time.Second),
		Burst:           10,
		MaxFailures:     3,
		LockoutDuration: time.Minute,
	}
	lim, clk, _ := newTestLimiter(t, []Profile{p})
	prof, _ := lim.Profile("auth")
	lim.RecordAuthFailure("1.1.1.1", prof)
	lim.RecordAuthFailure("1.1.1.1", prof)
	// Advance past TTL — next failure should start a fresh count.
	clk.Advance(failureTTL + time.Second)
	lim.RecordAuthFailure("1.1.1.1", prof)
	if locked, _ := lim.IsLockedOut("1.1.1.1"); locked {
		t.Fatal("should not lock out: prior failures were stale, count reset to 1")
	}
}

func TestStart_ExitsOnContextCancel(t *testing.T) {
	t.Parallel()
	p := Profile{Name: "default", Rate: rate.Every(time.Second), Burst: 1}
	lim, _, _ := newTestLimiter(t, []Profile{p})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { lim.Start(ctx, 10*time.Millisecond); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Start did not return after context cancel")
	}
}

func TestNewLimiter_RejectsDuplicateProfile(t *testing.T) {
	t.Parallel()
	_, err := NewLimiter(Config{
		Profiles: []Profile{
			{Name: "p", Rate: rate.Every(time.Second), Burst: 1},
			{Name: "p", Rate: rate.Every(time.Second), Burst: 1},
		},
		Metrics: observability.NewUnregisteredMetrics("test"),
		Logger:  zaptest.NewLogger(t),
	})
	if err == nil {
		t.Fatal("expected duplicate-name error")
	}
}

func TestNewLimiter_RejectsInvalidProfile(t *testing.T) {
	t.Parallel()
	_, err := NewLimiter(Config{
		Profiles: []Profile{{Name: "bad", Rate: 0, Burst: 1}},
		Metrics:  observability.NewUnregisteredMetrics("test"),
		Logger:   zaptest.NewLogger(t),
	})
	if err == nil {
		t.Fatal("expected rate<=0 error")
	}
}
