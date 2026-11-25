package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestLoginRejectsOversizedFieldsBeforeAuditing(t *testing.T) {
	h, db := auditTestHandler(t)
	for _, tc := range []struct {
		name, username, password string
		chunked                  bool
		want                     int
	}{
		{"username", strings.Repeat("x", domain.MaxUsernameBytes+1), "wrong", false, 400},
		{"password", "admin", strings.Repeat("x", domain.MaxPasswordBytes+1), false, 400},
		{"body", strings.Repeat("x", 100000), "wrong", false, 413},
		{"chunked body", strings.Repeat("x", 100000), "wrong", true, 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			form := url.Values{"username": {tc.username}, "password": {tc.password}, csrfFieldName: {"csrf-test"}}
			req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "csrf-test"})
			if tc.chunked {
				req.ContentLength = -1
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status=%d, want %d", rec.Code, tc.want)
			}
			var count int
			if err := db.QueryRow("SELECT COUNT(*) FROM auth_events").Scan(&count); err != nil || count != 0 {
				t.Fatalf("oversize request persisted an event: count=%d err=%v", count, err)
			}
		})
	}
	// Rejected input must not lock out a real account.
	loginAs(t, h, "admin", "password123")
}

func TestAuditFieldsBoundedWithoutInvalidUTF8(t *testing.T) {
	db := rbacDB(t)
	audit := store.NewAuthEventRepo(db)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = strings.Repeat("x", 1000)
	recordAuth(audit, req, domain.AuthSSOLogin, strings.Repeat("界", 1000), 1, strings.Repeat("界", 1000))
	events, err := audit.List(store.AuthEventFilter{})
	if err != nil || len(events) != 1 {
		t.Fatalf("events=%v err=%v", events, err)
	}
	e := events[0]
	if len(e.Username) > domain.MaxUsernameBytes || len(e.Detail) > 1024 || len(e.SourceIP) > 64 {
		t.Fatal("audit fields exceed their byte limits")
	}
	if !utf8.ValidString(e.Username) || !utf8.ValidString(e.Detail) {
		t.Fatal("truncation split a UTF-8 character")
	}
}

func TestRedirectRejectsURLControls(t *testing.T) {
	for _, c := range append([]byte{0x7f}, []byte("\x00\x01\x08\t\n\r\x1f")...) {
		for _, path := range []string{"/" + string(c) + "/example.invalid", "/hosts?q=" + string(c)} {
			if got := safeNext(path); got != "/" {
				t.Errorf("safeNext(%q)=%q", path, got)
			}
		}
	}
	h, session := authTestServer(t)
	for _, tc := range []struct{ path, form string }{
		{"/login", "username=admin&password=password123&next=%2F%09%2Fexample.invalid"},
		{"/theme", "theme=dark&return_to=%2F%09%2Fexample.invalid"},
	} {
		rec := postCSRF(t, h, session, tc.path, tc.form)
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
			t.Errorf("%s status=%d Location=%q", tc.path, rec.Code, rec.Header().Get("Location"))
		}
	}
	req := httptest.NewRequest(http.MethodPost, "http://example.com/theme", nil)
	req.Header.Set("Referer", "http://example.com/%09/example.invalid")
	if got := safeRedirectTarget(req); got != "/" {
		t.Fatalf("encoded Referer control survived: %q", got)
	}
}

func TestAuditRetentionIndependentOfAuthentication(t *testing.T) {
	for _, mode := range []string{"sso", "api", "health", "2fa"} {
		t.Run(mode, func(t *testing.T) {
			h, db := newProxyTestHandlerWithRetention(t, false, 90)
			raw := mkToken(t, db, "retention-user", domain.RoleAdmin, domain.ScopeReadOnly)
			audit := store.NewAuthEventRepo(db)
			old := time.Now().AddDate(0, 0, -100).UTC().Format(time.RFC3339)
			if err := audit.Create(domain.AuthEvent{Type: domain.AuthSSOLogin, Username: "retention-user", CreatedAt: old}); err != nil {
				t.Fatal(err)
			}
			path, want := "/hosts", http.StatusOK
			switch mode {
			case "api":
				path = "/api/hosts"
			case "health":
				path = "/healthz"
			case "2fa":
				path, want = "/login/2fa", http.StatusSeeOther
			}
			req := httptest.NewRequest(http.MethodGet, path, nil)
			if mode == "sso" {
				req.RemoteAddr = "10.0.0.5:1234"
				req.Header.Set("Remote-User", "retention-user")
			} else if mode == "api" {
				req.Header.Set("Authorization", "Bearer "+raw)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != want {
				t.Fatalf("status=%d want=%d", rec.Code, want)
			}
			var stale int
			if err := db.QueryRow("SELECT COUNT(*) FROM auth_events WHERE created_at=?", old).Scan(&stale); err != nil || stale != 0 {
				t.Fatalf("expired events=%d err=%v", stale, err)
			}
		})
	}
}

func TestAuditRetentionIntervalAndDisable(t *testing.T) {
	for _, days := range []int{0, 90} {
		t.Run(strconv.Itoa(days), func(t *testing.T) {
			db := rbacDB(t)
			audit := store.NewAuthEventRepo(db)
			now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
			old := now.AddDate(0, 0, -100).Format(time.RFC3339)
			seed := func() {
				if err := audit.Create(domain.AuthEvent{Type: domain.AuthLoginSuccess, CreatedAt: old}); err != nil {
					t.Fatal(err)
				}
			}
			count := func() int {
				var n int
				if err := db.QueryRow("SELECT COUNT(*) FROM auth_events").Scan(&n); err != nil {
					t.Fatal(err)
				}
				return n
			}
			h := auditRetention(audit, days, func() time.Time { return now })(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
			request := func() { h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil)) }
			seed()
			request()
			if days == 0 {
				if count() != 1 {
					t.Fatal("disabled retention deleted history")
				}
				return
			}
			if count() != 0 {
				t.Fatal("first request did not prune")
			}
			seed()
			var wg sync.WaitGroup
			for i := 0; i < 20; i++ {
				wg.Add(1)
				go func() { defer wg.Done(); request() }()
			}
			wg.Wait()
			if count() != 1 {
				t.Fatal("cleanup repeated within the same hour")
			}
			now = now.Add(time.Hour)
			request()
			if count() != 0 {
				t.Fatal("next interval did not prune")
			}
		})
	}
}
