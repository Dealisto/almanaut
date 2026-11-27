package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestHTMLMutationRechecksAuthenticationAfterBodyRead(t *testing.T) {
	for _, change := range []string{"session revoked", "session expired", "admin demoted", "session storage failure"} {
		t.Run(change, func(t *testing.T) {
			h, db := totpTestHandler(t)
			session := loginAs(t, h, "admin", "password123")
			users, sessions, tokens := store.NewUserRepo(db), store.NewSessionRepo(db), store.NewTokenRepo(db)
			id, err := users.Create(domain.User{Username: "target", PasswordHash: "unused", Role: domain.RoleViewer, CreatedAt: nowRFC3339(), UpdatedAt: nowRFC3339()})
			if err != nil {
				t.Fatal(err)
			}
			path, form := "/account/tokens", url.Values{"label": {"stale session"}, "scope": {string(domain.ScopeReadWrite)}}
			want := http.StatusSeeOther
			if change == "admin demoted" {
				path, form = "/users/"+itoa(id)+"/role", url.Values{"role": {string(domain.RoleAdmin)}}
				want = http.StatusForbidden
			} else if change == "session storage failure" {
				want = http.StatusInternalServerError
			}
			form.Set(csrfFieldName, "valid-csrf")
			reader := &beforeBodyRead{Reader: strings.NewReader(form.Encode()), before: func() {
				var err error
				switch change {
				case "session revoked":
					err = sessions.DeleteByToken(hashToken(session.Value))
				case "session expired":
					_, err = db.Exec(`UPDATE sessions SET expires_at='2000-01-01T00:00:00Z'`)
				case "admin demoted":
					err = users.UpdateRole(1, domain.RoleViewer, nowRFC3339())
				case "session storage failure":
					_, err = db.Exec(`DROP TABLE sessions`)
				}
				if err != nil {
					t.Fatal(err)
				}
			}}
			req := httptest.NewRequest(http.MethodPost, path, reader)
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.AddCookie(session)
			req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "valid-csrf"})
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != want {
				t.Errorf("mutation status=%d, want %d", rec.Code, want)
			}
			if want == http.StatusSeeOther && !strings.HasPrefix(rec.Header().Get("Location"), "/login?") {
				t.Errorf("revoked authentication did not require login: %q", rec.Header().Get("Location"))
			}
			if want == http.StatusInternalServerError && (rec.Header().Get("Location") != "" || strings.Contains(rec.Body.String(), "sessions")) {
				t.Errorf("storage error disguised as login failure or disclosed: %s", rec.Body)
			}
			if issued, err := tokens.ListByUser(1); err != nil || len(issued) != 0 {
				t.Errorf("revoked session minted new tokens: %+v %v", issued, err)
			}
			if target, err := users.Get(id); err != nil || target.Role != domain.RoleViewer {
				t.Errorf("demoted admin changed another role: %+v %v", target, err)
			}
			for _, kind := range []string{domain.AuthTokenCreated, domain.AuthRoleChanged} {
				if events, err := store.NewAuthEventRepo(db).List(store.AuthEventFilter{Type: kind}); err != nil || len(events) != 0 {
					t.Errorf("stale authorization recorded success: %+v %v", events, err)
				}
			}
		})
	}
}

func TestHTMLMutationRefreshesSSOIdentityWithoutSessionFallback(t *testing.T) {
	for _, change := range []string{"unchanged", "demoted", "deleted"} {
		t.Run(change, func(t *testing.T) {
			h, db := newProxyTestHandler(t, true)
			users := store.NewUserRepo(db)
			id, err := users.Create(domain.User{Username: "alice", Role: domain.RoleEditor, PasswordHash: "unused", CreatedAt: nowRFC3339(), UpdatedAt: nowRFC3339()})
			if err != nil {
				t.Fatal(err)
			}
			otherID, err := users.Create(domain.User{Username: "other", Role: domain.RoleAdmin, PasswordHash: "unused", CreatedAt: nowRFC3339(), UpdatedAt: nowRFC3339()})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.NewSessionRepo(db).Create(store.Session{TokenHash: hashToken("other-session"), UserID: otherID, CreatedAt: nowRFC3339(), ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}); err != nil {
				t.Fatal(err)
			}
			reader := &beforeBodyRead{Reader: strings.NewReader("csrf_token=valid-csrf&name=new-host&type=physical"), before: func() {
				var err error
				switch change {
				case "demoted":
					err = users.UpdateRole(id, domain.RoleViewer, nowRFC3339())
				case "deleted":
					err = users.Delete(id)
				}
				if err != nil {
					t.Fatal(err)
				}
			}}
			req := httptest.NewRequest(http.MethodPost, "/hosts", reader)
			req.RemoteAddr = "10.0.0.5:1234"
			req.Header.Set("Remote-User", "alice")
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "valid-csrf"})
			req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "other-session"})
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			wantStatus, wantCount := http.StatusSeeOther, 0
			if change == "unchanged" {
				wantCount = 1
			} else if change == "demoted" {
				wantStatus = http.StatusForbidden
			}
			if rec.Code != wantStatus {
				t.Errorf("SSO mutation status=%d, want %d", rec.Code, wantStatus)
			}
			if change == "deleted" {
				if !strings.HasPrefix(rec.Header().Get("Location"), "/login?") {
					t.Errorf("deleted SSO user did not require login: %q", rec.Header().Get("Location"))
				}
				if _, err := users.GetByUsername("alice"); err != store.ErrNotFound {
					t.Errorf("deleted SSO user was provisioned again: %v", err)
				}
			}
			if hosts, err := store.NewHostRepo(db).List(); err != nil || len(hosts) != wantCount {
				t.Errorf("SSO used stale permissions or another browser identity: hosts=%v err=%v", hosts, err)
			}
			if change == "unchanged" {
				events, err := store.NewChangelogRepo(db).ListRecent(10)
				if err != nil || len(events) != 1 || events[0].Actor != "alice" {
					t.Errorf("SSO mutation lost actor: events=%v err=%v", events, err)
				}
			}
		})
	}
}

type beforeBodyRead struct {
	io.Reader
	before func()
}

func (r *beforeBodyRead) Read(p []byte) (int, error) {
	if r.before != nil {
		before := r.before
		r.before = nil
		before()
	}
	return r.Reader.Read(p)
}
