package web

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func accessAuditEvents(t *testing.T, db *sql.DB) []domain.AuthEvent {
	t.Helper()
	all, err := store.NewAuthEventRepo(db).List(store.AuthEventFilter{})
	if err != nil {
		t.Fatal(err)
	}
	var events []domain.AuthEvent
	for _, event := range all {
		switch event.Type {
		case domain.AuthRoleChanged, domain.AuthTokenCreated, domain.AuthTokenRevoked,
			domain.AuthTokenRotationStarted, domain.AuthTokenRotationCompleted:
			events = append(events, event)
		}
	}
	return events
}

func auditExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func TestAccountAccessAuditRecordsCommittedLifecycleWithoutSecrets(t *testing.T) {
	for _, brokenAudit := range []bool{false, true} {
		t.Run(fmt.Sprintf("audit_failure=%v", brokenAudit), func(t *testing.T) {
			h, db := auditTestHandler(t)
			admin := loginAs(t, h, "admin", "password123")
			seedUserAndLogin(t, h, db, "target", domain.RoleViewer)
			if brokenAudit {
				auditExec(t, db, `CREATE TRIGGER fail_access_audit BEFORE INSERT ON auth_events BEGIN SELECT RAISE(ABORT, 'audit unavailable'); END`)
			}
			post := func(path, body string, status int) *httptest.ResponseRecorder {
				t.Helper()
				rec := postCSRF(t, h, admin, path, body)
				if rec.Code != status {
					t.Fatalf("POST %s = %d, want %d: %s", path, rec.Code, status, rec.Body)
				}
				return rec
			}
			post("/users/2/role", "role=editor", http.StatusSeeOther)
			rec := post("/account/tokens", "label=PRIVATE_LABEL&scope=read-only&expiry_days=7", http.StatusOK)
			originalSecret := rawTokenRE.FindString(rec.Body.String())
			if originalSecret == "" {
				t.Fatal("created token not shown")
			}
			tokens := store.NewTokenRepo(db)
			list, err := tokens.ListByUser(1)
			if err != nil || len(list) != 1 {
				t.Fatalf("created tokens: %+v, %v", list, err)
			}
			original := list[0]
			rec = post(fmt.Sprintf("/account/tokens/%d/rotate", original.ID), "expiry_days=30", http.StatusOK)
			replacementSecret := rawTokenRE.FindString(rec.Body.String())
			if replacementSecret == "" {
				t.Fatal("replacement token not shown")
			}
			list, err = tokens.ListByUser(1)
			if err != nil || len(list) != 2 {
				t.Fatalf("rotated tokens: %+v, %v", list, err)
			}
			replacement := list[0]
			auditExec(t, db, "UPDATE api_tokens SET last_used_at=? WHERE id=?", nowRFC3339(), replacement.ID)
			post(fmt.Sprintf("/account/tokens/%d/finish-rotation", replacement.ID), "", http.StatusSeeOther)
			post(fmt.Sprintf("/account/tokens/%d/delete", replacement.ID), "", http.StatusSeeOther)
			if list, err := tokens.ListByUser(1); err != nil || len(list) != 0 {
				t.Fatalf("token lifecycle did not finish: %+v %v", list, err)
			}
			if target, err := store.NewUserRepo(db).Get(2); err != nil || target.Role != domain.RoleEditor {
				t.Fatalf("role change did not persist: %+v %v", target, err)
			}
			events := accessAuditEvents(t, db)
			if brokenAudit {
				if len(events) != 0 {
					t.Fatalf("failing audit unexpectedly recorded events: %+v", events)
				}
				return
			}
			want := []struct {
				kind, detail, username string
				userID                 int64
			}{
				{domain.AuthTokenRevoked, fmt.Sprintf("token_id=%d", replacement.ID), "admin", 1},
				{domain.AuthTokenRotationCompleted, fmt.Sprintf("revoked_token_id=%d replacement_token_id=%d", original.ID, replacement.ID), "admin", 1},
				{domain.AuthTokenRotationStarted, fmt.Sprintf("original_token_id=%d replacement_token_id=%d expires_at=%s", original.ID, replacement.ID, replacement.ExpiresAt), "admin", 1},
				{domain.AuthTokenCreated, fmt.Sprintf("token_id=%d scope=read-only expires_at=%s", original.ID, original.ExpiresAt), "admin", 1},
				{domain.AuthRoleChanged, "from=viewer to=editor actor_user_id=1", "target", 2},
			}
			if len(events) != len(want) {
				t.Fatalf("audit events: %+v", events)
			}
			for i, e := range events {
				w := want[i]
				if e.Type != w.kind || e.Detail != w.detail || e.UserID != w.userID || e.Username != w.username || e.SourceIP == "" || e.CreatedAt == "" {
					t.Errorf("event %d = %+v; want %+v", i, e, w)
				}
				for _, secret := range []string{originalSecret, replacementSecret, original.TokenHash, replacement.TokenHash, "PRIVATE_LABEL", "password123"} {
					if strings.Contains(fmt.Sprint(e), secret) {
						t.Error("audit event contains credential or arbitrary label data")
					}
				}
			}
			page := httptest.NewRecorder()
			h.ServeHTTP(page, withCookie(httptest.NewRequest(http.MethodGet, "/audit?type="+domain.AuthRoleChanged, nil), admin))
			if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "from=viewer to=editor actor_user_id=1") {
				t.Fatal("new access events are not visible in the filtered audit page")
			}
			for _, event := range want {
				if !strings.Contains(page.Body.String(), `value="`+event.kind+`"`) {
					t.Errorf("event filter omits %s", event.kind)
				}
			}
		})
	}
}

func TestAccountAccessAuditSkipsRejectedNoopAndRolledBackChanges(t *testing.T) {
	h, db := auditTestHandler(t)
	admin := loginAs(t, h, "admin", "password123")
	viewer := seedUserAndLogin(t, h, db, "target", domain.RoleViewer)
	tokens := store.NewTokenRepo(db)
	seed := func(owner, original int64, hash string) int64 {
		t.Helper()
		id, err := tokens.Create(store.APIToken{UserID: owner, TokenHash: hash, Label: "private", Scope: "read-only", CreatedAt: nowRFC3339(), RotatedFromID: original})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	original := seed(1, 0, "original")
	replacement := seed(1, original, "replacement")
	other := seed(2, 0, "other")
	post := func(session *http.Cookie, path, body string, status int) {
		t.Helper()
		rec := postCSRF(t, h, session, path, body)
		if rec.Code != status {
			t.Fatalf("POST %s = %d, want %d: %s", path, rec.Code, status, rec.Body)
		}
		if events := accessAuditEvents(t, db); len(events) != 0 {
			t.Fatalf("rejected/no-op request recorded success: %+v", events)
		}
	}
	post(admin, "/users/2/role", "role=viewer", 303)
	post(admin, "/users/1/role", "role=viewer", 200) // last admin
	post(viewer, "/users/1/role", "role=viewer", 403)
	post(admin, "/users/999/role", "role=viewer", 404)
	post(admin, "/account/tokens", "label=bad&scope=invalid", 200)
	post(admin, fmt.Sprintf("/account/tokens/%d/rotate", other), "", 404)
	post(admin, fmt.Sprintf("/account/tokens/%d/finish-rotation", other), "", 404)
	post(admin, fmt.Sprintf("/account/tokens/%d/delete", other), "", 303)
	post(admin, "/account/tokens/999/delete", "", 303)
	post(admin, fmt.Sprintf("/account/tokens/%d/rotate", original), "", 409)
	post(admin, fmt.Sprintf("/account/tokens/%d/finish-rotation", replacement), "", 409)
	post(admin, fmt.Sprintf("/account/tokens/%d/finish-rotation", original), "", 409)

	// Inject storage failures after validation; none may produce success logs.
	auditExec(t, db, `CREATE TRIGGER fail_role BEFORE UPDATE OF role ON users BEGIN SELECT RAISE(ABORT, 'role unavailable'); END`)
	post(admin, "/users/2/role", "role=editor", 500)
	unrotated := seed(1, 0, "unrotated")
	auditExec(t, db, `CREATE TRIGGER fail_token_insert BEFORE INSERT ON api_tokens BEGIN SELECT RAISE(ABORT, 'insert unavailable'); END`)
	post(admin, "/account/tokens", "label=test&scope=read-only", 500)
	post(admin, fmt.Sprintf("/account/tokens/%d/rotate", unrotated), "", 500)
	auditExec(t, db, "UPDATE api_tokens SET last_used_at=? WHERE id=?", nowRFC3339(), replacement)
	auditExec(t, db, `CREATE TRIGGER fail_token_delete BEFORE DELETE ON api_tokens BEGIN SELECT RAISE(ABORT, 'delete unavailable'); END`)
	post(admin, fmt.Sprintf("/account/tokens/%d/delete", original), "", 500)
	post(admin, fmt.Sprintf("/account/tokens/%d/finish-rotation", replacement), "", 500)
	if list, err := tokens.ListByUser(1); err != nil || len(list) != 3 {
		t.Fatalf("failed writes changed token state: %+v %v", list, err)
	}
}

func TestAccountAccessAuditConcurrentIdenticalChangesRecordOnce(t *testing.T) {
	h, db := auditTestHandler(t)
	admin := loginAs(t, h, "admin", "password123")
	seedUserAndLogin(t, h, db, "target", domain.RoleViewer)
	tokens := store.NewTokenRepo(db)
	original, err := tokens.Create(store.APIToken{UserID: 1, TokenHash: "original", Label: "test", Scope: "read-only", CreatedAt: nowRFC3339()})
	if err != nil {
		t.Fatal(err)
	}
	run := func(path, body, kind string, wantStatus, repeatStatus int) {
		t.Helper()
		var wg sync.WaitGroup
		codes := make(chan int, 8)
		for range 8 {
			wg.Go(func() { codes <- postCSRF(t, h, admin, path, body).Code })
		}
		wg.Wait()
		close(codes)
		for code := range codes {
			if code != wantStatus && code != repeatStatus {
				t.Fatalf("concurrent %s status = %d", path, code)
			}
		}
		events, err := store.NewAuthEventRepo(db).List(store.AuthEventFilter{Type: kind})
		if err != nil || len(events) != 1 {
			t.Fatalf("concurrent %s recorded %+v: %v", path, events, err)
		}
	}
	run("/users/2/role", "role=editor", domain.AuthRoleChanged, 303, 303)
	run(fmt.Sprintf("/account/tokens/%d/rotate", original), "", domain.AuthTokenRotationStarted, 200, 409)
	list, err := tokens.ListByUser(1)
	if err != nil || len(list) != 2 {
		t.Fatalf("rotation results: %+v %v", list, err)
	}
	replacement := list[0].ID
	auditExec(t, db, "UPDATE api_tokens SET last_used_at=? WHERE id=?", nowRFC3339(), replacement)
	run(fmt.Sprintf("/account/tokens/%d/finish-rotation", replacement), "", domain.AuthTokenRotationCompleted, 303, 409)
	run(fmt.Sprintf("/account/tokens/%d/delete", replacement), "", domain.AuthTokenRevoked, 303, 303)
}
