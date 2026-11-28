package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/backup"
	"github.com/Dealisto/almanaut/internal/domain"
)

type testBackupLister struct {
	items []backup.Manifest
	err   error
}

func (l testBackupLister) List(context.Context) ([]backup.Manifest, error) { return l.items, l.err }

func TestDatabaseBackupsRequiresAdmin(t *testing.T) {
	db := rbacDB(t)
	handler := newAuthedTestHandler(t, db)
	for _, role := range []domain.Role{domain.RoleViewer, domain.RoleEditor, domain.RoleAdmin} {
		cookie := seedUserAndLogin(t, handler, db, string(role), role)
		rec := getWith(t, handler, cookie, "/database-backups")
		want := http.StatusForbidden
		if role == domain.RoleAdmin {
			want = http.StatusOK
			if !strings.Contains(rec.Body.String(), "Scheduled database backups are not configured") {
				t.Fatal("disabled backup configuration should explain setup")
			}
		}
		if rec.Code != want {
			t.Errorf("%s status = %d, want %d", role, rec.Code, want)
		}
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/database-backups", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("anonymous access = %d", rec.Code)
	}
}

func TestDatabaseBackupsListsStoredManifestsWithoutTriggeringJobs(t *testing.T) {
	runner := &fakeRunner{}
	lister := testBackupLister{items: []backup.Manifest{{
		Directory: "almanaut-backup-123", Source: "/private/source-must-not-be-rendered.db",
		ServerVersion: "dev <script>", CreatedAt: time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC),
		Verification: backup.Verification{Bytes: 123456, SchemaVersion: "0048_host_agent_protection.sql"},
	}}}
	rec := httptest.NewRecorder()
	databaseBackupsPage(lister, runner).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/database-backups", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"almanaut-backup-123", "123456", "dev &lt;script&gt;", "2026-11-27 12:00:00", "/tasks/database-backup/run"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	if len(runner.triggered) != 0 || strings.Contains(body, "source-must-not-be-rendered") || strings.Contains(body, "dev <script>") {
		t.Fatal("read-only backup page triggered work or exposed unsafe data")
	}
}

func TestDatabaseBackupsStorageFailureIsNotAnEmptyHistory(t *testing.T) {
	rec := httptest.NewRecorder()
	databaseBackupsPage(testBackupLister{err: errors.New("private-directory-read-failure")}, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/database-backups", nil))
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "private-directory-read-failure") {
		t.Fatalf("unexpected error response: %d %s", rec.Code, rec.Body.String())
	}
}
