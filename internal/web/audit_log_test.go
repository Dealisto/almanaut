package web

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

// failedLogin drives a login POST expected to fail (401).
func failedLogin(t *testing.T, h http.Handler, username, password string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/login", nil))
	csrf := csrfCookie(rec.Result().Cookies())
	form := strings.NewReader("username=" + username + "&password=" + password + "&" + csrfFieldName + "=" + csrf.Value)
	req := httptest.NewRequest(http.MethodPost, "/login", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrf)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusUnauthorized {
		t.Fatalf("failed login code = %d, want 401", rec2.Code)
	}
}

func auditTestHandler(t *testing.T) (http.Handler, *sql.DB) {
	t.Helper()
	db := rbacDB(t)
	if err := BootstrapAdmin(store.NewUserRepo(db), testLogger(), "admin", "password123", false); err != nil {
		t.Fatalf("BootstrapAdmin: %v", err)
	}
	return newAuthedTestHandler(t, db), db
}

func TestAuditLogRecordsLoginEvents(t *testing.T) {
	h, _ := auditTestHandler(t)

	failedLogin(t, h, "admin", "wrong-password")
	admin := loginAs(t, h, "admin", "password123") // records a login_success

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, withCookie(httptest.NewRequest(http.MethodGet, "/audit", nil), admin))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /audit = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, domain.AuthLoginSuccess) || !strings.Contains(body, domain.AuthLoginFailure) {
		t.Errorf("audit log missing login events:\n%s", body)
	}

	// Filtering by type narrows to failures only.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, withCookie(httptest.NewRequest(http.MethodGet, "/audit?type="+domain.AuthLoginFailure, nil), admin))
	if b := rec.Body.String(); !strings.Contains(b, domain.AuthLoginFailure) {
		t.Errorf("filtered audit missing failures:\n%s", b)
	}
}

func TestAuditLogAdminOnly(t *testing.T) {
	h, db := auditTestHandler(t)
	viewer := seedUserAndLogin(t, h, db, "vic", domain.RoleViewer)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, withCookie(httptest.NewRequest(http.MethodGet, "/audit", nil), viewer))
	if rec.Code != http.StatusForbidden {
		t.Errorf("viewer GET /audit = %d, want 403", rec.Code)
	}
}

func TestTokenUseLogCoalesces(t *testing.T) {
	l := newTokenUseLog()
	now := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	if !l.shouldLog("h1", now) {
		t.Fatal("first use should log")
	}
	if l.shouldLog("h1", now.Add(time.Minute)) {
		t.Error("within window should not log again")
	}
	if !l.shouldLog("h1", now.Add(2*time.Hour)) {
		t.Error("after window should log again")
	}
}
