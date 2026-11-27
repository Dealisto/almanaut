package web

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

// clientIP returns the direct peer's IP (host part of RemoteAddr). We record the
// direct peer rather than a spoofable X-Forwarded-For, so the audit log cannot
// be poisoned by a forged header.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// recordAuth appends an authentication audit event, filling in the source IP and
// timestamp. Auditing must never break the auth flow, so a write failure is
// logged and swallowed rather than propagated. A nil repo (tests that don't wire
// auditing) is a no-op.
func recordAuth(audit *store.AuthEventRepo, r *http.Request, eventType, username string, userID int64, detail string) {
	if audit == nil {
		return
	}
	err := audit.Create(domain.AuthEvent{
		Type: eventType, Username: boundedAuditText(username, domain.MaxUsernameBytes), UserID: userID,
		SourceIP: boundedAuditText(clientIP(r), 64), Detail: boundedAuditText(detail, 1024), CreatedAt: nowRFC3339(),
	})
	if err != nil {
		loggerFrom(r.Context()).Printf("audit: record %s failed: %v", eventType, err)
	}
}

func boundedAuditText(s string, limit int) string {
	if len(s) > limit {
		s = s[:limit]
	}
	return strings.ToValidUTF8(s, "")
}

// auditRetention performs maintenance on the first request and at most hourly
// thereafter, independent of password, SSO, API or 2FA authentication. Health
// probes also trigger it. Reserving the interval before the database work keeps
// concurrent requests and database failures from causing a cleanup stampede.
func auditRetention(audit *store.AuthEventRepo, retentionDays int, now func() time.Time) func(http.Handler) http.Handler {
	var mu sync.Mutex
	var nextRun time.Time
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if audit != nil && retentionDays > 0 {
				t := now()
				mu.Lock()
				due := !t.Before(nextRun)
				if due {
					nextRun = t.Add(time.Hour)
				}
				mu.Unlock()
				if due {
					pruneAuditOpportunistically(audit, r, t, retentionDays)
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// pruneAuditOpportunistically deletes audit events older than the retention
// window. Called by shared request maintenance so the table stays bounded
// without a dedicated job. retentionDays <= 0 disables pruning. A nil
// repo or a delete failure is logged and ignored.
func pruneAuditOpportunistically(audit *store.AuthEventRepo, r *http.Request, now time.Time, retentionDays int) {
	if audit == nil || retentionDays <= 0 {
		return
	}
	cutoff := now.AddDate(0, 0, -retentionDays).UTC().Format(time.RFC3339)
	if _, err := audit.Prune(cutoff); err != nil {
		loggerFrom(r.Context()).Printf("audit: prune failed: %v", err)
	}
}

// tokenUseLog coalesces API-token-use audit events so a busy client does not
// flood the log: each token is recorded at most once per window. In-memory and
// reset on restart, which is fine for a coarse-grained usage trail.
type tokenUseLog struct {
	mu     sync.Mutex
	seen   map[string]time.Time
	window time.Duration
}

func newTokenUseLog() *tokenUseLog {
	return &tokenUseLog{seen: map[string]time.Time{}, window: time.Hour}
}

// shouldLog reports whether this token's use should be recorded now, updating the
// last-seen time when it returns true.
func (l *tokenUseLog) shouldLog(tokenHash string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if last, ok := l.seen[tokenHash]; ok && now.Sub(last) < l.window {
		return false
	}
	// Opportunistically drop entries older than the window so the map stays
	// bounded by the number of recently-active tokens.
	for k, t := range l.seen {
		if now.Sub(t) >= l.window {
			delete(l.seen, k)
		}
	}
	l.seen[tokenHash] = now
	return true
}

type auditLogData struct {
	Title      string
	Events     []domain.AuthEvent
	Types      []string
	Users      []string
	FilterUser string
	FilterType string
	Since      string
	Until      string
}

// auditEventTypes is the closed set offered in the filter dropdown.
var auditEventTypes = []string{
	domain.AuthLoginSuccess, domain.AuthLoginFailure, domain.AuthLogout,
	domain.Auth2FASuccess, domain.Auth2FAFailure, domain.AuthSSOLogin,
	domain.AuthTokenUsed, domain.AuthSessionRevoked,
	domain.AuthRoleChanged, domain.AuthTokenCreated, domain.AuthTokenRevoked,
	domain.AuthTokenRotationStarted, domain.AuthTokenRotationCompleted,
}

// auditLogPage renders the admin-only authentication audit log with filters.
func auditLogPage(audit *store.AuthEventRepo, users *store.UserRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		q := req.URL.Query()
		filter := store.AuthEventFilter{
			Username: strings.TrimSpace(q.Get("user")),
			Type:     strings.TrimSpace(q.Get("type")),
			Since:    strings.TrimSpace(q.Get("since")),
			Until:    strings.TrimSpace(q.Get("until")),
		}
		if n, err := strconv.Atoi(q.Get("limit")); err == nil && n > 0 {
			filter.Limit = min(n, 5000) // cap so a huge limit can't materialize the whole table
		}
		events, err := audit.List(filter)
		if err != nil {
			serverError(w, req, err)
			return
		}
		userList, err := users.List()
		if err != nil {
			serverError(w, req, err)
			return
		}
		names := make([]string, 0, len(userList))
		for _, u := range userList {
			names = append(names, u.Username)
		}
		render(w, req, "audit.html", auditLogData{
			Title:      "Audit log",
			Events:     events,
			Types:      auditEventTypes,
			Users:      names,
			FilterUser: filter.Username,
			FilterType: filter.Type,
			Since:      filter.Since,
			Until:      filter.Until,
		})
	}
}
