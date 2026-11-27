package web

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

// Pause after reading the entity, before the handler can record its event.
// A separate SQLite connection attempts deletion at precisely that boundary.
type acknowledgementReadRepo struct {
	*store.HostRepo
	afterRead func()
}

func (r acknowledgementReadRepo) List() ([]domain.Host, error) {
	items, err := r.HostRepo.List()
	if err == nil {
		r.afterRead()
	}
	return items, err
}

func (r acknowledgementReadRepo) GetTx(tx *sql.Tx, id int64) (domain.Host, error) {
	item, err := r.HostRepo.GetTx(tx, id)
	if err == nil {
		r.afterRead()
	}
	return item, err
}

func TestAcknowledgeSerializesExistenceCheckWithDeletion(t *testing.T) {
	rs, deps := hostResourceForTest(t)
	id, err := rs.repo.Create(domain.Host{Name: "keep-until-acknowledged", Type: "vm"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := deps.db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	// Fail immediately if the acknowledgement already owns the write lock.
	if _, err := other.ExecContext(context.Background(), "PRAGMA busy_timeout=0"); err != nil {
		t.Fatal(err)
	}
	var deleteErr error
	attempted := false
	rs.repo = acknowledgementReadRepo{HostRepo: store.NewHostRepo(deps.db), afterRead: func() {
		attempted = true
		_, deleteErr = other.ExecContext(context.Background(), "DELETE FROM hosts WHERE id=?", id)
	}}
	req := httptest.NewRequest(http.MethodPost, "/health-report/acknowledge", strings.NewReader("ref=host:"+itoa(id)))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	acknowledgeStale(entityCatalog{resources: []mountable{rs}}, deps.changelog, deps.db)(rec, req)
	if !attempted {
		t.Fatal("did not exercise the competing deletion")
	}
	if deleteErr == nil || !strings.Contains(deleteErr.Error(), "SQLITE_BUSY") {
		t.Errorf("deletion was not excluded while acknowledging: %v", deleteErr)
	}
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("acknowledge = %d, want 303: %s", rec.Code, rec.Body)
	}
	if _, err := store.NewHostRepo(deps.db).Get(id); err != nil {
		t.Errorf("acknowledged a deleted entity: %v", err)
	}
	events, err := deps.changelog.ListForEntity("host", id)
	if err != nil || len(events) != 1 || events[0].Label != "host: keep-until-acknowledged" {
		t.Fatalf("acknowledgement history = %+v, %v", events, err)
	}
}

func TestAcknowledgeDoesNotReadUnrelatedInventory(t *testing.T) {
	srv, db := newTestServerDB(t)
	id, err := store.NewHostRepo(db).Create(domain.Host{Name: "still-readable", Type: "vm"})
	if err != nil {
		t.Fatal(err)
	}
	// An unreadable, unrelated inventory table must not prevent acknowledging
	// a host. The handler needs only the requested row and its current label.
	if _, err := db.Exec("DROP TABLE contacts"); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/health-report/acknowledge", strings.NewReader("ref=host:"+itoa(id)+"&csrf_token="+csrfTestToken))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: csrfTestToken})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("unrelated inventory prevented acknowledgement: status=%d body=%s", rec.Code, rec.Body)
	}
}
