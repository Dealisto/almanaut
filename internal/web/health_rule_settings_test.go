package web

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func onlyHealthRules(ids ...string) domain.HealthRuleSettings {
	settings := domain.DefaultHealthRuleSettings()
	for _, rule := range domain.HealthRuleDefinitions {
		settings.Enabled[rule.ID] = false
	}
	for _, id := range ids {
		settings.Enabled[id] = true
	}
	return settings
}

func TestHealthRuleSettingsAdminSaveResetAndValidation(t *testing.T) {
	db := rbacDB(t)
	srv := newAuthedTestHandler(t, db)
	admin := seedUserAndLogin(t, srv, db, "admin", domain.RoleAdmin)
	for _, role := range []domain.Role{domain.RoleViewer, domain.RoleEditor} {
		cookie := seedUserAndLogin(t, srv, db, string(role), role)
		if rec := getWith(t, srv, cookie, "/health-rules"); rec.Code != http.StatusForbidden {
			t.Fatalf("%s settings GET=%d", role, rec.Code)
		}
		for _, path := range []string{"/health-rules", "/health-rules/reset"} {
			if rec := csrfPostRec(t, srv, cookie, path, "settings_present=1&certificate_warning_days=30"); rec.Code != http.StatusForbidden {
				t.Fatalf("%s settings POST=%d", role, rec.Code)
			}
		}
	}
	if rec := getWith(t, srv, admin, "/health-rules"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Use server setting") {
		t.Fatalf("admin settings = %d", rec.Code)
	}
	rec := csrfPostRec(t, srv, admin, "/health-rules", "settings_present=1&enabled=hosts_without_backup&stale_after_days=14&certificate_warning_days=7")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("save=%d %s", rec.Code, rec.Body.String())
	}
	settings, err := store.NewHealthRuleSettingsRepo(db).Get()
	if err != nil || settings.StaleAfterDays == nil || *settings.StaleAfterDays != 14 || settings.CertificateWarningDays != 7 || !settings.RuleEnabled("hosts_without_backup") || settings.RuleEnabled("expired_certificates") {
		t.Fatalf("saved=%+v %v", settings, err)
	}
	events, err := store.NewAuthEventRepo(db).List(store.AuthEventFilter{Type: "health_rules_updated"})
	if err != nil || len(events) != 1 || events[0].Username != "admin" || !strings.Contains(events[0].Detail, `"Before"`) {
		t.Fatalf("settings audit=%+v %v", events, err)
	}
	for _, form := range []string{"settings_present=1&enabled=unknown&certificate_warning_days=30", "settings_present=1&certificate_warning_days=0", "settings_present=1&stale_after_days=not-a-number&certificate_warning_days=30", "certificate_warning_days=30"} {
		rec := csrfPostRec(t, srv, admin, "/health-rules", form)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("invalid form=%d %s", rec.Code, rec.Body.String())
		}
		after, _ := store.NewHealthRuleSettingsRepo(db).Get()
		if !reflect.DeepEqual(after, settings) {
			t.Fatal("invalid form mutated settings")
		}
	}
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/health-rules/reset", nil)
	req.AddCookie(admin)
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("reset without CSRF=%d", rec.Code)
	}
	if rec := csrfPostRec(t, srv, admin, "/health-rules/reset", ""); rec.Code != http.StatusSeeOther {
		t.Fatalf("reset=%d", rec.Code)
	}
	after, _ := store.NewHealthRuleSettingsRepo(db).Get()
	if !reflect.DeepEqual(after, domain.DefaultHealthRuleSettings()) {
		t.Fatal("reset lost default inheritance")
	}
	events, err = store.NewAuthEventRepo(db).List(store.AuthEventFilter{Type: "health_rules_reset"})
	if err != nil || len(events) != 1 {
		t.Fatal("reset not audited", err)
	}
}

func TestHealthRuleSettingsSharedReportAndDashboard(t *testing.T) {
	srv, db := newStaleTestServer(t, 90)
	if rec := postForm(t, srv, "/hosts", url.Values{"name": {"nas"}, "type": {"physical"}}); rec.Code != 303 {
		t.Fatal(rec.Code)
	}
	settings := onlyHealthRules("hosts_without_backup")
	if err := store.NewHealthRuleSettingsRepo(db).Save(settings); err != nil {
		t.Fatal(err)
	}
	report := getBody(t, srv, "/health-report")
	dashboard := getBody(t, srv, "/")
	if !strings.Contains(report, "Hosts without a backup") || strings.Contains(report, "Orphaned entities") || !strings.Contains(dashboard, "1 inventory issue") {
		t.Fatal("report/dashboard disagree after disabling checks")
	}
	settings.Enabled["hosts_without_backup"] = false
	if err := store.NewHealthRuleSettingsRepo(db).Save(settings); err != nil {
		t.Fatal(err)
	}
	report = getBody(t, srv, "/health-report")
	dashboard = getBody(t, srv, "/")
	if !strings.Contains(report, "No checks enabled") || strings.Contains(dashboard, "1 inventory issue") {
		t.Fatal("disabled finding still counted")
	}
}

func TestHealthRuleSettingsStalenessOverrideAndCertificateWindow(t *testing.T) {
	srv, db := newStaleTestServer(t, 90)
	if rec := postForm(t, srv, "/hosts", url.Values{"name": {"nas"}, "type": {"physical"}}); rec.Code != 303 {
		t.Fatal(rec.Code)
	}
	if _, err := db.Exec("UPDATE changelog SET created_at=? WHERE entity_type='host'", time.Now().AddDate(0, 0, -20).UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	settings := onlyHealthRules("stale_entities")
	repo := store.NewHealthRuleSettingsRepo(db)
	if err := repo.Save(settings); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(getBody(t, srv, "/health-report"), `value="host:1"`) {
		t.Fatal("blank override did not inherit90days")
	}
	days := 7
	settings.StaleAfterDays = &days
	if err := repo.Save(settings); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(getBody(t, srv, "/health-report"), `value="host:1"`) {
		t.Fatal("7-day override not applied")
	}
	days = 0
	if err := repo.Save(settings); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(getBody(t, srv, "/health-report"), `value="host:1"`) {
		t.Fatal("zero did not disable staleness")
	}
	settings = onlyHealthRules("certificates_due_soon")
	settings.CertificateWarningDays = 10
	for _, tc := range []struct {
		name string
		days int
	}{{"expired", -1}, {"today", 0}, {"inside-window", 10}, {"outside-window", 11}} {
		if _, err := store.NewCertificateRepo(db).Create(domain.Certificate{Subject: tc.name, ExpiresOn: time.Now().AddDate(0, 0, tc.days).Format(domain.DateLayout)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.Save(settings); err != nil {
		t.Fatal(err)
	}
	report := getBody(t, srv, "/health-report")
	for _, want := range []string{"Certificates expiring soon", "inside-window"} {
		if !strings.Contains(report, want) {
			t.Fatal("missing due-soon finding", want)
		}
	}
	for _, notWanted := range []string{">expired</a>", ">today</a>", ">outside-window</a>"} {
		if strings.Contains(report, notWanted) {
			t.Fatal("wrong certificate boundary", notWanted)
		}
	}
}

func TestHealthRuleSettingsBackupEvidenceRule(t *testing.T) {
	srv, db := newStaleTestServer(t, 90)
	hours := int64(24)
	now := time.Now().UTC()
	for _, tc := range []struct {
		name, status string
		age          time.Duration
		monitored    bool
	}{{"never-job", "", 0, true}, {"failed-job", "failed", time.Hour, true}, {"stale-job", "success", 48 * time.Hour, true}, {"healthy-job", "success", time.Hour, true}, {"unmonitored-job", "", 0, false}} {
		backup := domain.Backup{Source: tc.name, LastRun: now.Format(domain.DateLayout)}
		if tc.monitored {
			backup.ExpectedIntervalHours = &hours
		}
		id, err := store.NewBackupRepo(db).Create(backup)
		if err != nil {
			t.Fatal(err)
		}
		if tc.status != "" {
			err := store.WithTx(db, func(tx *sql.Tx) error {
				_, _, err := store.NewBackupRunRepo(db).RecordTx(tx, domain.BackupRun{BackupID: id, RunID: "run-1", Status: tc.status, CompletedAt: now.Add(-tc.age).Format(time.RFC3339)}, now)
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	settings := onlyHealthRules("backup-job-health")
	if err := store.NewHealthRuleSettingsRepo(db).Save(settings); err != nil {
		t.Fatal(err)
	}
	report := getBody(t, srv, "/health-report")
	for _, name := range []string{"never-job", "failed-job", "stale-job"} {
		if !strings.Contains(report, name) {
			t.Fatal("missing backup finding", name)
		}
	}
	for _, name := range []string{"healthy-job", "unmonitored-job"} {
		if strings.Contains(report, name) {
			t.Fatal("false backup finding", name)
		}
	}
	if !strings.Contains(getBody(t, srv, "/"), "3 inventory issues") {
		t.Fatal("backup findings absent from shared dashboard evaluator")
	}
	settings.Enabled["backup-job-health"] = false
	if err := store.NewHealthRuleSettingsRepo(db).Save(settings); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(getBody(t, srv, "/health-report"), "never-job") {
		t.Fatal("disabled backup rule still produces findings")
	}
}

func TestHealthRuleSettingsAuditFailureRollsBack(t *testing.T) {
	db := rbacDB(t)
	srv := newAuthedTestHandler(t, db)
	admin := seedUserAndLogin(t, srv, db, "admin", domain.RoleAdmin)
	if _, err := db.Exec(`CREATE TRIGGER reject_health_settings_audit BEFORE INSERT ON auth_events WHEN NEW.event_type='health_rules_updated' BEGIN SELECT RAISE(ABORT,'audit unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	rec := csrfPostRec(t, srv, admin, "/health-rules", "settings_present=1&stale_after_days=14&certificate_warning_days=7")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("audit failure=%d", rec.Code)
	}
	settings, err := store.NewHealthRuleSettingsRepo(db).Get()
	if err != nil || !reflect.DeepEqual(settings, domain.DefaultHealthRuleSettings()) {
		t.Fatalf("audit failure committed policy: %+v %v", settings, err)
	}
}
