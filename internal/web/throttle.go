package web

import (
	"strings"
	"sync"
	"time"
)

// Login throttling policy: after loginFailureThreshold failed logins for a
// username, further attempts are refused for loginLockoutDuration. The state is
// per-username and in-memory (reset on process restart), which is sufficient
// for a single-binary homelab deployment.
const (
	loginFailureThreshold = 5
	loginLockoutDuration  = 15 * time.Minute
)

// attemptState tracks failed logins for one username key.
type attemptState struct {
	failures    int
	lockedUntil time.Time
	lastSeen    time.Time // last failed attempt; cleanup drops idle entries
}

// loginThrottle rate-limits failed logins per username. All methods are safe
// for concurrent use. It keys on a normalised (trimmed, lower-cased) username
// so case variants share one bucket; the key is used only for counting and does
// not affect how credentials are looked up.
type loginThrottle struct {
	mu        sync.Mutex
	attempts  map[string]*attemptState
	threshold int
	cooldown  time.Duration
}

// newLoginThrottle returns a throttle using the package default policy.
func newLoginThrottle() *loginThrottle {
	return newLoginThrottleWith(loginFailureThreshold, loginLockoutDuration)
}

// newLoginThrottleWith returns a throttle with an explicit policy (used by tests
// to avoid depending on the production threshold/cooldown values).
func newLoginThrottleWith(threshold int, cooldown time.Duration) *loginThrottle {
	return &loginThrottle{
		attempts:  make(map[string]*attemptState),
		threshold: threshold,
		cooldown:  cooldown,
	}
}

// throttleKey normalises a username so case and surrounding whitespace do not
// create separate buckets.
func throttleKey(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}

// allowed reports whether a login attempt for username may proceed. When the
// username is locked out it returns false and the time remaining until the
// lockout expires. Call this before verifying the password so a locked account
// pays no bcrypt cost.
func (t *loginThrottle) allowed(username string, now time.Time) (bool, time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	st := t.attempts[throttleKey(username)]
	if st == nil {
		return true, 0
	}
	if now.Before(st.lockedUntil) {
		return false, st.lockedUntil.Sub(now)
	}
	return true, 0
}

// recordFailure counts a failed login. On reaching the threshold it arms a
// lockout and resets the counter, so the next lockout requires another full run
// of failures after this one expires.
func (t *loginThrottle) recordFailure(username string, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	key := throttleKey(username)
	st := t.attempts[key]
	if st == nil {
		st = &attemptState{}
		t.attempts[key] = st
	}
	st.failures++
	st.lastSeen = now
	if st.failures >= t.threshold {
		st.lockedUntil = now.Add(t.cooldown)
		st.failures = 0
	}
}

// recordSuccess clears any failure state for username (a successful login wipes
// the slate).
func (t *loginThrottle) recordSuccess(username string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.attempts, throttleKey(username))
}

// cleanup drops entries that are no longer locked and have been idle for at
// least the cooldown, bounding memory. It is called opportunistically after a
// successful login, mirroring sessions.DeleteExpired.
func (t *loginThrottle) cleanup(now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for key, st := range t.attempts {
		if now.Before(st.lockedUntil) {
			continue // still locked — keep
		}
		if now.Sub(st.lastSeen) >= t.cooldown {
			delete(t.attempts, key)
		}
	}
}
