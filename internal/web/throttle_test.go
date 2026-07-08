package web

import (
	"sync"
	"testing"
	"time"
)

func TestLoginThrottleAllowsBelowThreshold(t *testing.T) {
	th := newLoginThrottleWith(3, time.Minute)
	now := time.Unix(0, 0).UTC()
	th.recordFailure("bob", now)
	th.recordFailure("bob", now)
	if ok, _ := th.allowed("bob", now); !ok {
		t.Fatal("bob locked out below threshold")
	}
}

func TestLoginThrottleLocksAtThreshold(t *testing.T) {
	th := newLoginThrottleWith(3, time.Minute)
	now := time.Unix(0, 0).UTC()
	for i := 0; i < 3; i++ {
		th.recordFailure("bob", now)
	}
	ok, retry := th.allowed("bob", now)
	if ok {
		t.Fatal("bob not locked after reaching threshold")
	}
	if retry <= 0 || retry > time.Minute {
		t.Fatalf("retryAfter = %v, want (0, 1m]", retry)
	}
}

func TestLoginThrottleUnlocksAfterCooldown(t *testing.T) {
	th := newLoginThrottleWith(3, time.Minute)
	now := time.Unix(0, 0).UTC()
	for i := 0; i < 3; i++ {
		th.recordFailure("bob", now)
	}
	later := now.Add(time.Minute + time.Second)
	if ok, _ := th.allowed("bob", later); !ok {
		t.Fatal("bob still locked after cooldown expired")
	}
}

func TestLoginThrottleSuccessResets(t *testing.T) {
	th := newLoginThrottleWith(3, time.Minute)
	now := time.Unix(0, 0).UTC()
	th.recordFailure("bob", now)
	th.recordFailure("bob", now)
	th.recordSuccess("bob")
	th.recordFailure("bob", now) // counter was cleared, so this is #1 again
	th.recordFailure("bob", now) // #2 — still below threshold 3
	if ok, _ := th.allowed("bob", now); !ok {
		t.Fatal("counter not reset after success")
	}
}

func TestLoginThrottleKeyNormalised(t *testing.T) {
	th := newLoginThrottleWith(2, time.Minute)
	now := time.Unix(0, 0).UTC()
	th.recordFailure("  Bob ", now)
	th.recordFailure("BOB", now)
	if ok, _ := th.allowed("bob", now); ok {
		t.Fatal("case/whitespace variants should share one bucket and lock")
	}
}

func TestLoginThrottleCleanup(t *testing.T) {
	th := newLoginThrottleWith(3, time.Minute)
	now := time.Unix(0, 0).UTC()
	th.recordFailure("idle", now) // partial count, will go idle
	for i := 0; i < 3; i++ {      // locked entry
		th.recordFailure("locked", now)
	}
	later := now.Add(time.Minute + time.Second) // past cooldown
	th.cleanup(later)

	th.mu.Lock()
	_, idleExists := th.attempts["idle"]
	_, lockedExists := th.attempts["locked"]
	th.mu.Unlock()
	if idleExists {
		t.Error("idle entry should have been cleaned up")
	}
	if lockedExists {
		t.Error("expired lockout should have been cleaned up too")
	}

	// A still-active lockout is retained.
	for i := 0; i < 3; i++ {
		th.recordFailure("active", later)
	}
	th.cleanup(later.Add(time.Second))
	th.mu.Lock()
	_, activeExists := th.attempts["active"]
	th.mu.Unlock()
	if !activeExists {
		t.Error("active lockout should be retained by cleanup")
	}
}

func TestLoginThrottleConcurrent(t *testing.T) {
	th := newLoginThrottleWith(1000, time.Minute) // high threshold: never locks here
	now := time.Unix(0, 0).UTC()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			th.recordFailure("bob", now)
			th.allowed("bob", now)
		}()
	}
	wg.Wait()
	th.mu.Lock()
	got := th.attempts["bob"].failures
	th.mu.Unlock()
	if got != 100 {
		t.Fatalf("failures = %d, want 100 (lost updates indicate a data race)", got)
	}
}
