package kuma

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"path/filepath"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
	"github.com/Dealisto/almanaut/internal/webhook"
)

type staticServices []domain.Service

func (s staticServices) List() ([]domain.Service, error) { return s, nil }

func newSyncerDB(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.Migrate(db, dbPath); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return db
}

func TestReconcileEndToEnd(t *testing.T) {
	f := newFakeKuma(t)
	db := newSyncerDB(t)
	mapping := store.NewKumaRepo(db)
	handMade := f.putMonitor("hand-made", "http://keep.me")

	services := staticServices{
		{ID: 1, Name: "jellyfin", Kind: "container", URL: "http://jellyfin.lan:8096"},
		{ID: 2, Name: "no-url", Kind: "native"},
	}
	y := NewSyncer(NewClient(f.url(), "admin", "s3cret", false), services, mapping, log.Default())

	sum := y.Reconcile(context.Background())
	if sum.Err != nil {
		t.Fatalf("Reconcile: %v", sum.Err)
	}
	if sum.Created != 1 || sum.Updated != 0 || sum.Deleted != 0 || sum.Skipped != 1 {
		t.Fatalf("summary = %+v", sum)
	}
	if f.monitorCount() != 2 { // hand-made + jellyfin
		t.Fatalf("monitorCount = %d, want 2", f.monitorCount())
	}
	all, _ := mapping.All()
	if len(all) != 1 || all[1] == 0 {
		t.Fatalf("mapping = %v", all)
	}
	wantMonitorID := all[1]

	// Idempotence: same input → no actions, no duplicates.
	sum = y.Reconcile(context.Background())
	if sum.Err != nil || sum.Created+sum.Updated+sum.Deleted != 0 {
		t.Fatalf("second pass not a no-op: %+v", sum)
	}
	if f.monitorCount() != 2 {
		t.Fatalf("second pass duplicated monitors: %d", f.monitorCount())
	}
	all, _ = mapping.All()
	if len(all) != 1 || all[1] != wantMonitorID {
		t.Fatalf("mapping after idempotent pass = %v, want {1: %d}", all, wantMonitorID)
	}

	// Edit + delete: rename service 1's URL, drop nothing else.
	y2 := NewSyncer(NewClient(f.url(), "admin", "s3cret", false),
		staticServices{{ID: 1, Name: "jellyfin", Kind: "container", URL: "http://jellyfin.lan:9999"}},
		mapping, log.Default())
	sum = y2.Reconcile(context.Background())
	if sum.Err != nil || sum.Updated != 1 {
		t.Fatalf("edit pass = %+v", sum)
	}

	// Service gone → managed monitor deleted; hand-made survives.
	y3 := NewSyncer(NewClient(f.url(), "admin", "s3cret", false), staticServices{}, mapping, log.Default())
	sum = y3.Reconcile(context.Background())
	if sum.Err != nil || sum.Deleted != 1 {
		t.Fatalf("delete pass = %+v", sum)
	}
	if _, ok := f.getMonitor(handMade); !ok {
		t.Fatal("hand-made monitor was touched")
	}
	all, _ = mapping.All()
	if len(all) != 0 {
		t.Fatalf("mapping not emptied: %v", all)
	}

	last := y3.LastSync()
	if !last.Ran || last.Time.IsZero() {
		t.Fatalf("LastSync = %+v", last)
	}
}

// failingPuts wraps a mappingStore and makes Put always fail, to exercise the
// actCreate compensating-delete path.
type failingPuts struct {
	mappingStore
	err error
}

func (f failingPuts) Put(serviceID, monitorID int64) error { return f.err }

func TestReconcileCreateMappingFailureDeletesOrphan(t *testing.T) {
	f := newFakeKuma(t)
	db := newSyncerDB(t)
	realMapping := store.NewKumaRepo(db)
	putErr := errors.New("mapping write failed")
	failing := failingPuts{mappingStore: realMapping, err: putErr}

	services := staticServices{
		{ID: 1, Name: "jellyfin", Kind: "container", URL: "http://jellyfin.lan:8096"},
	}
	y := NewSyncer(NewClient(f.url(), "admin", "s3cret", false), services, failing, log.Default())

	sum := y.Reconcile(context.Background())
	if sum.Err == nil {
		t.Fatal("expected an error when the mapping write fails")
	}
	if sum.Created != 0 {
		t.Fatalf("Created = %d, want 0", sum.Created)
	}
	if f.monitorCount() != 0 {
		t.Fatalf("monitorCount = %d, want 0 (compensating delete should remove the orphan)", f.monitorCount())
	}
	all, _ := realMapping.All()
	if len(all) != 0 {
		t.Fatalf("mapping = %v, want empty", all)
	}

	// Re-run with the real (non-failing) repo: exactly one monitor should
	// exist afterward — no duplicate left behind by the failed first pass.
	y2 := NewSyncer(NewClient(f.url(), "admin", "s3cret", false), services, realMapping, log.Default())
	sum = y2.Reconcile(context.Background())
	if sum.Err != nil {
		t.Fatalf("Reconcile: %v", sum.Err)
	}
	if sum.Created != 1 {
		t.Fatalf("Created = %d, want 1", sum.Created)
	}
	if f.monitorCount() != 1 {
		t.Fatalf("monitorCount = %d, want 1 (no duplicate)", f.monitorCount())
	}
}

func TestReconcileKumaDownIsAnErrorNotAPanic(t *testing.T) {
	f := newFakeKuma(t)
	url := f.url()
	f.srv.Close()
	db := newSyncerDB(t)
	y := NewSyncer(NewClient(url, "admin", "s3cret", false), staticServices{}, store.NewKumaRepo(db), log.Default())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if sum := y.Reconcile(ctx); sum.Err == nil {
		t.Fatal("expected an error with Kuma down")
	}
}

func TestDispatchFiltersAndCoalesces(t *testing.T) {
	db := newSyncerDB(t)
	y := NewSyncer(NewClient("http://unused.lan", "u", "p", false), staticServices{}, store.NewKumaRepo(db), log.Default())
	// Not started: triggers just accumulate in the (size-1) channel.
	y.Dispatch(webhook.Event{Type: "host", Action: webhook.ActionCreated})
	if len(y.trigger) != 0 {
		t.Fatal("host event must not trigger a sync")
	}
	y.Dispatch(webhook.Event{Type: "service", Action: webhook.ActionCreated})
	y.Dispatch(webhook.Event{Type: "service", Action: webhook.ActionUpdated})
	y.Dispatch(webhook.Event{Type: "service", Action: webhook.ActionDeleted})
	if len(y.trigger) != 1 {
		t.Fatalf("triggers did not coalesce: %d", len(y.trigger))
	}
}
