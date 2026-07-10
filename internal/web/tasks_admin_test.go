package web

import (
	"database/sql"
	"io"
	"log"
	"net/http"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/job"
	"github.com/Dealisto/almanaut/internal/store"
)

// fakeRunner is a jobRunner for the admin-page tests.
type fakeRunner struct {
	statuses  []job.Status
	triggered []string
}

func (f *fakeRunner) Statuses() []job.Status { return f.statuses }
func (f *fakeRunner) Trigger(name string) bool {
	f.triggered = append(f.triggered, name)
	return name == "expiry-notifications"
}

func newTasksTestHandler(t *testing.T, db *sql.DB, runner jobRunner) http.Handler {
	t.Helper()
	return New(Config{
		Hosts: store.NewHostRepo(db), Services: store.NewServiceRepo(db), Networks: store.NewNetworkRepo(db),
		Domains: store.NewDomainRepo(db), Certificates: store.NewCertificateRepo(db), Backups: store.NewBackupRepo(db),
		Hardware: store.NewHardwareRepo(db), Subscriptions: store.NewSubscriptionRepo(db), Accounts: store.NewAccountRepo(db),
		Sites: store.NewSiteRepo(db), Locations: store.NewLocationRepo(db), Racks: store.NewRackRepo(db),
		Contacts:      store.NewContactRepo(db),
		Relationships: store.NewRelationshipRepo(db), Tags: store.NewTagRepo(db), VLANs: store.NewVLANRepo(db), Reservations: store.NewReservationRepo(db), DB: db,
		Logger: log.New(io.Discard, "", 0),
		Docker: fakeScanner{}, NetScan: fakeNetworkScanner{}, NetOpts: NetDiscoveryOptions{}, Proxmox: fakeProxmoxScanner{}, PVEOpts: ProxmoxOptions{},
		AuthEnabled: true,
		Tasks:       runner,
	})
}

func TestTasksPageListsJobs(t *testing.T) {
	db := rbacDB(t)
	runner := &fakeRunner{statuses: []job.Status{{Name: "expiry-notifications", Title: "Expiry notifications"}}}
	h := newTasksTestHandler(t, db, runner)
	admin := seedUserAndLogin(t, h, db, "admin", domain.RoleAdmin)

	rec := getWith(t, h, admin, "/tasks")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /tasks = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Expiry notifications") {
		t.Fatalf("page should list the job title; body: %s", rec.Body.String())
	}
}

func TestTasksPageRequiresAdmin(t *testing.T) {
	db := rbacDB(t)
	runner := &fakeRunner{}
	h := newTasksTestHandler(t, db, runner)
	viewer := seedUserAndLogin(t, h, db, "viewer", domain.RoleViewer)
	if rec := getWith(t, h, viewer, "/tasks"); rec.Code != http.StatusForbidden {
		t.Fatalf("GET /tasks as viewer = %d, want 403", rec.Code)
	}
}

func TestTasksRunTriggersJob(t *testing.T) {
	db := rbacDB(t)
	runner := &fakeRunner{}
	h := newTasksTestHandler(t, db, runner)
	admin := seedUserAndLogin(t, h, db, "admin", domain.RoleAdmin)

	rec := csrfPostRec(t, h, admin, "/tasks/expiry-notifications/run", "")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST run = %d, want 303", rec.Code)
	}
	if len(runner.triggered) != 1 || runner.triggered[0] != "expiry-notifications" {
		t.Fatalf("Trigger not called correctly: %v", runner.triggered)
	}
}
