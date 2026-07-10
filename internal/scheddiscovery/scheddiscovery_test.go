package scheddiscovery

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/discovery"
	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/notify"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestFreshKeys(t *testing.T) {
	got := FreshKeys([]string{"a", "b"}, []string{"b", "c"})
	if len(got) != 1 || got[0] != "c" {
		t.Fatalf("want [c], got %v", got)
	}
}

type fakeDocker struct {
	cs  []discovery.Container
	err error
}

func (f fakeDocker) Containers(context.Context) ([]discovery.Container, error) { return f.cs, f.err }

type svcList []domain.Service

func (s svcList) List() ([]domain.Service, error) { return []domain.Service(s), nil }

type countSender struct {
	mu sync.Mutex
	n  int
}

func (c *countSender) Send(context.Context, notify.Notification) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n++
	return nil
}
func (c *countSender) count() int { c.mu.Lock(); defer c.mu.Unlock(); return c.n }

func testStore(t *testing.T) *store.DiscoveryRunRepo {
	t.Helper()
	path := filepath.Join(t.TempDir(), "t.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(db, path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return store.NewDiscoveryRunRepo(db)
}

func TestRunDockerDiffNotifies(t *testing.T) {
	runs := testStore(t)
	sender := &countSender{}
	now := func() time.Time { return time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC) }
	docker := fakeDocker{cs: []discovery.Container{{ID: "c1", Name: "grafana"}, {ID: "c2", Name: "prometheus"}}}
	// "grafana" already tracked → only c2 (prometheus) is new.
	d := New(docker, nil, "", nil, svcList{{Name: "grafana", Kind: "container"}}, nil, runs, sender, nil, now)

	if err := d.RunDocker(context.Background()); err != nil {
		t.Fatal(err)
	}
	if sender.count() != 1 {
		t.Fatalf("first run should notify once, got %d", sender.count())
	}
	latest, _ := runs.Latest("docker")
	if latest.NewCount != 1 {
		t.Fatalf("want new_count 1, got %d", latest.NewCount)
	}

	// Same containers again → no new notification.
	if err := d.RunDocker(context.Background()); err != nil {
		t.Fatal(err)
	}
	if sender.count() != 1 {
		t.Fatalf("second identical run must not re-notify, got %d", sender.count())
	}
}

type mutableDocker struct {
	mu  sync.Mutex
	cs  []discovery.Container
	err error
}

func (m *mutableDocker) Containers(context.Context) ([]discovery.Container, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cs, m.err
}
func (m *mutableDocker) set(cs []discovery.Container, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cs, m.err = cs, err
}

type mutableSvcList struct {
	mu  sync.Mutex
	svc []domain.Service
}

func (m *mutableSvcList) List() ([]domain.Service, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]domain.Service(nil), m.svc...), nil
}
func (m *mutableSvcList) set(svc []domain.Service) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.svc = svc
}

// TestFailedRunDoesNotResetNotificationBaseline reproduces the notification
// storm defect: a transient scan failure between two successful runs must not
// cause a still-untracked finding to be re-notified on recovery.
func TestFailedRunDoesNotResetNotificationBaseline(t *testing.T) {
	runs := testStore(t)
	sender := &countSender{}
	now := func() time.Time { return time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC) }
	docker := &mutableDocker{cs: []discovery.Container{{ID: "c1", Name: "grafana"}, {ID: "c2", Name: "prometheus"}}}
	d := New(docker, nil, "", nil, svcList{{Name: "grafana", Kind: "container"}}, nil, runs, sender, nil, now)

	// Run 1: success, c2 (prometheus) is new → notifies once.
	if err := d.RunDocker(context.Background()); err != nil {
		t.Fatalf("run1: %v", err)
	}
	if sender.count() != 1 {
		t.Fatalf("run1 should notify once, got %d", sender.count())
	}

	// Run 2: transient scan failure (e.g. docker socket down).
	docker.set(nil, errors.New("docker daemon unreachable"))
	if err := d.RunDocker(context.Background()); err == nil {
		t.Fatal("run2 should return the scan error")
	}
	if sender.count() != 1 {
		t.Fatalf("run2 (failed scan) must not notify, got %d", sender.count())
	}
	failedRun, err := runs.Latest("docker")
	if err != nil || failedRun.Error == "" {
		t.Fatalf("run2 should be recorded with an error: %+v, err=%v", failedRun, err)
	}

	// Run 3: recovery, same still-untracked c2 → must NOT re-notify.
	docker.set([]discovery.Container{{ID: "c1", Name: "grafana"}, {ID: "c2", Name: "prometheus"}}, nil)
	if err := d.RunDocker(context.Background()); err != nil {
		t.Fatalf("run3: %v", err)
	}
	if sender.count() != 1 {
		t.Fatalf("run3 (recovery, same finding) must not re-notify, got %d", sender.count())
	}
}

// TestNewFindingAfterImportStillNotifies confirms the diff baseline fix does
// not suppress genuinely-new findings: once c2 is imported (tracked), a
// newly-appeared c3 must still trigger exactly one more notification.
func TestNewFindingAfterImportStillNotifies(t *testing.T) {
	runs := testStore(t)
	sender := &countSender{}
	now := func() time.Time { return time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC) }
	docker := &mutableDocker{cs: []discovery.Container{{ID: "c1", Name: "grafana"}, {ID: "c2", Name: "prometheus"}}}
	svc := &mutableSvcList{svc: []domain.Service{{Name: "grafana", Kind: "container"}}}
	d := New(docker, nil, "", nil, svc, nil, runs, sender, nil, now)

	// Run 1: c2 (prometheus) is new → notifies once.
	if err := d.RunDocker(context.Background()); err != nil {
		t.Fatalf("run1: %v", err)
	}
	if sender.count() != 1 {
		t.Fatalf("run1 should notify once, got %d", sender.count())
	}

	// c2 gets imported, and a new container c3 (loki) appears.
	svc.set([]domain.Service{{Name: "grafana", Kind: "container"}, {Name: "prometheus", Kind: "container"}})
	docker.set([]discovery.Container{
		{ID: "c1", Name: "grafana"}, {ID: "c2", Name: "prometheus"}, {ID: "c3", Name: "loki"},
	}, nil)
	if err := d.RunDocker(context.Background()); err != nil {
		t.Fatalf("run2: %v", err)
	}
	if sender.count() != 2 {
		t.Fatalf("run2 (genuinely new c3) should notify once more, got %d", sender.count())
	}
}

func TestRunDockerScanErrorRecordedAndReturned(t *testing.T) {
	runs := testStore(t)
	sender := &countSender{}
	d := New(fakeDocker{err: errors.New("socket down")}, nil, "", nil, svcList{}, nil, runs, sender, nil, nil)
	err := d.RunDocker(context.Background())
	if err == nil {
		t.Fatal("want the scan error returned")
	}
	if sender.count() != 0 {
		t.Fatal("must not notify on scan error")
	}
	latest, _ := runs.Latest("docker")
	if latest.Error == "" {
		t.Fatalf("scan error not recorded: %+v", latest)
	}
}
