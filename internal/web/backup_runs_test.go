package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func sendBackupReport(srv http.Handler, id int64, body, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/backups/"+itoa(id)+"/runs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}
func backupReportBody(runID, status string) string {
	body, _ := json.Marshal(map[string]any{"run_id": runID, "status": status, "completed_at": time.Now().UTC().Truncate(time.Second).Format(time.RFC3339), "size_bytes": 0})
	return string(body)
}

func TestBackupRunReportIdempotencyAuditAndUI(t *testing.T) {
	srv, db, dispatcher := newTestServerWHDB(t)
	hours := int64(24)
	id, err := store.NewBackupRepo(db).Create(domain.Backup{Source: "external", ExpectedIntervalHours: &hours})
	if err != nil {
		t.Fatal(err)
	}
	body := backupReportBody("nightly-1", "success")
	first := sendBackupReport(srv, id, body, "")
	if first.Code != http.StatusCreated {
		t.Fatalf("report=%d %s", first.Code, first.Body)
	}
	retry := sendBackupReport(srv, id, body, "")
	if retry.Code != http.StatusOK || retry.Body.String() != first.Body.String() {
		t.Fatalf("retry=%d %s", retry.Code, retry.Body)
	}
	conflict := sendBackupReport(srv, id, strings.Replace(body, "success", "failed", 1), "")
	if conflict.Code != http.StatusConflict {
		t.Fatalf("conflict=%d", conflict.Code)
	}
	history, _ := store.NewChangelogRepo(db).ListRecent(10)
	runs, _ := store.NewBackupRunRepo(db).List(context.Background(), id)
	if len(history) != 1 || len(runs) != 1 || len(dispatcher.all()) != 1 {
		t.Fatalf("history=%+v runs=%+v events=%+v", history, runs, dispatcher.all())
	}
	backup, _ := store.NewBackupRepo(db).Get(id)
	if backup.LastRun != "" {
		t.Fatalf("report overwrote documented date: %+v", backup)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/backups/"+itoa(id)+"/runs", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Job health: healthy") || !strings.Contains(rec.Body.String(), "nightly-1") {
		t.Fatalf("page=%d %s", rec.Code, rec.Body)
	}
	rec = getJSONResp(t, srv, "/api/backups/"+itoa(id)+"/runs")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "nightly-1") {
		t.Fatalf("list=%d %s", rec.Code, rec.Body)
	}
}

func TestBackupRunReportAuthorization(t *testing.T) {
	db := newWebTestDB(t)
	srv := newAuthedTestHandler(t, db)
	id, err := store.NewBackupRepo(db).Create(domain.Backup{Source: "external"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		role  domain.Role
		scope domain.Scope
		want  int
	}{{"viewer", domain.RoleViewer, domain.ScopeReadWrite, 403}, {"readonly", domain.RoleEditor, domain.ScopeReadOnly, 403}, {"agent", domain.RoleAdmin, domain.ScopeAgent, 403}, {"editor", domain.RoleEditor, domain.ScopeReadWrite, 201}} {
		t.Run(tc.name, func(t *testing.T) {
			token := mkToken(t, db, tc.name, tc.role, tc.scope)
			rec := sendBackupReport(srv, id, backupReportBody(tc.name, "success"), token)
			if rec.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%s", rec.Code, tc.want, rec.Body)
			}
		})
	}
	if rec := sendBackupReport(srv, id, backupReportBody("anonymous", "success"), ""); rec.Code != 401 {
		t.Fatalf("anonymous=%d", rec.Code)
	}
}

func TestBackupRunReportQueueFailureRollsBack(t *testing.T) {
	srv, db, dispatcher := newOutboxWebServer(t, 1)
	id, err := store.NewBackupRepo(db).Create(domain.Backup{Source: "external"})
	if err != nil {
		t.Fatal(err)
	}
	if rec := sendBackupReport(srv, id, backupReportBody("one", "success"), ""); rec.Code != 201 {
		t.Fatalf("first=%d %s", rec.Code, rec.Body)
	}
	if rec := sendBackupReport(srv, id, backupReportBody("two", "failed"), ""); rec.Code != 503 {
		t.Fatalf("second=%d %s", rec.Code, rec.Body)
	}
	runs, _ := store.NewBackupRunRepo(db).List(context.Background(), id)
	history, _ := store.NewChangelogRepo(db).ListRecent(10)
	pending, err := store.NewWebhookOutboxRepo(db).PendingCount()
	if err != nil || len(runs) != 1 || len(history) != 1 || len(dispatcher.all()) != 1 || pending != 1 {
		t.Fatalf("runs=%+v history=%+v events=%+v pending=%d err=%v", runs, history, dispatcher.all(), pending, err)
	}
}

func TestBackupRunRejectsInvalidReports(t *testing.T) {
	srv, db := newTestServerDB(t)
	id, err := store.NewBackupRepo(db).Create(domain.Backup{Source: "external"})
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{`null`, `{}`, `{"run_id":"one","status":"running","completed_at":"2026-01-01T00:00:00Z"}`, `{"run_id":"one","status":"success","completed_at":"2026-01-01T00:00:00Z","secret":"ignored"}`, backupReportBody("one", "success") + ` {}`} {
		rec := sendBackupReport(srv, id, body, "")
		if rec.Code != 400 {
			t.Fatalf("invalid=%d body=%s", rec.Code, rec.Body)
		}
	}
	runs, _ := store.NewBackupRunRepo(db).List(context.Background(), id)
	if len(runs) != 0 {
		t.Fatal("invalid reports persisted")
	}
}

func TestBackupCSVPolicyPatchOmitAndClear(t *testing.T) {
	srv, db := newTestServerDB(t)
	hours := int64(24)
	id, err := store.NewBackupRepo(db).Create(domain.Backup{Source: "external", ExpectedIntervalHours: &hours})
	if err != nil {
		t.Fatal(err)
	}
	token := csvReviewToken(t, uploadCSVPreview(t, srv, "backup", "id,notes\n"+itoa(id)+",new\n", "patch"))
	if rec := confirmCSVPreview(t, srv, token); rec.Code != 303 {
		t.Fatalf("apply=%d %s", rec.Code, rec.Body)
	}
	got, _ := store.NewBackupRepo(db).Get(id)
	if got.ExpectedIntervalHours == nil || *got.ExpectedIntervalHours != 24 {
		t.Fatalf("omission lost policy: %+v", got)
	}
	token = csvReviewToken(t, uploadCSVPreview(t, srv, "backup", "id,expected_interval_hours\n"+itoa(id)+",\n", "patch"))
	if rec := confirmCSVPreview(t, srv, token); rec.Code != 303 {
		t.Fatalf("clear=%d %s", rec.Code, rec.Body)
	}
	got, _ = store.NewBackupRepo(db).Get(id)
	if got.ExpectedIntervalHours == nil || *got.ExpectedIntervalHours != 0 {
		t.Fatalf("empty did not clear: %+v", got)
	}
	rec := uploadCSVPreview(t, srv, "backup", "id,expected_interval_hours\n"+itoa(id)+",nonsense\n", "patch")
	if rec.Code != 400 {
		t.Fatalf("malformed policy=%d %s", rec.Code, rec.Body)
	}
}
