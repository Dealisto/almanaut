package scheddiscovery

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
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

type failingSender struct{ err error }

func (s failingSender) Send(context.Context, notify.Notification) error { return s.err }

func TestRunReturnsNotificationFailureAndKeepsScanHistory(t *testing.T) {
	runs := testStore(t)
	sendErr := errors.New("notification unavailable")
	d := New(fakeDocker{cs: []discovery.Container{{ID: "c1", Name: "new"}}}, nil, "", nil, svcList{}, nil, runs, failingSender{sendErr}, nil, nil)
	if err := d.RunDocker(context.Background()); !errors.Is(err, sendErr) {
		t.Fatalf("notification error was lost: %v", err)
	}
	latest, err := runs.Latest("docker")
	if err != nil || latest.NewCount != 1 || !strings.Contains(latest.Error, sendErr.Error()) {
		t.Fatalf("scan results and delivery failure must survive in history: %+v, %v", latest, err)
	}
	if _, err := runs.LatestSuccessful("docker"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("failed delivery must not advance the notification baseline: %v", err)
	}
}

type retrySender struct {
	calls int
	err   error
}

func (s *retrySender) Send(context.Context, notify.Notification) error {
	s.calls++
	return s.err
}

func TestFailedNotificationRetriesUntilDelivered(t *testing.T) {
	for _, sourceScoped := range []bool{false, true} {
		for _, withBaseline := range []bool{false, true} {
			name := fmt.Sprintf("source=%t/baseline=%t", sourceScoped, withBaseline)
			t.Run(name, func(t *testing.T) {
				runs := testStore(t)
				sender := &retrySender{}
				docker := &mutableDocker{cs: []discovery.Container{{ID: "old", Name: "old"}}}
				key := "docker"
				makeRun := func() func(context.Context) error {
					if sourceScoped {
						key = "docker:lab"
						return NewSource(Source{Key: key, Kind: "docker", Docker: docker}, svcList{}, nil, &sourceBindings{}, runs, sender).Run
					}
					return New(docker, nil, "", nil, svcList{}, nil, runs, sender, nil, nil).RunDocker
				}
				run := makeRun()
				var baselineID int64
				if withBaseline {
					if err := run(context.Background()); err != nil {
						t.Fatal(err)
					}
					baseline, err := runs.LatestSuccessful(key)
					if err != nil {
						t.Fatal(err)
					}
					baselineID = baseline.ID
				}
				docker.set([]discovery.Container{{ID: "old", Name: "old"}, {ID: "new", Name: "new"}}, nil)
				sender.err = errors.New("temporary delivery outage")
				if err := run(context.Background()); !errors.Is(err, sender.err) {
					t.Fatalf("delivery error = %v, want %v", err, sender.err)
				}
				latest, err := runs.Latest(key)
				if err != nil || latest.Error == "" || latest.FoundCount != 2 || latest.NewCount != 2 {
					t.Fatalf("failed run must preserve scan results and error: %+v, %v", latest, err)
				}
				baseline, err := runs.LatestSuccessful(key)
				if withBaseline {
					if err != nil || baseline.ID != baselineID {
						t.Fatalf("failed delivery advanced baseline: %+v, %v", baseline, err)
					}
				} else if !errors.Is(err, store.ErrNotFound) {
					t.Fatalf("failed first delivery created a baseline: %v", err)
				}
				attempts := sender.calls
				sender.err = nil
				// A fresh detector models a restart: retry state must be durable.
				run = makeRun()
				if err := run(context.Background()); err != nil {
					t.Fatal(err)
				}
				if sender.calls != attempts+1 {
					t.Fatalf("delivery was not retried: attempts=%d, want %d", sender.calls, attempts+1)
				}
				if err := run(context.Background()); err != nil {
					t.Fatal(err)
				}
				if sender.calls != attempts+1 {
					t.Fatal("successful delivery must suppress unchanged findings")
				}
			})
		}
	}
}

func TestRunReturnsPersistenceFailureWithoutSuppressingNotification(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runs.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := store.Migrate(db, path); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER fail_discovery_run BEFORE INSERT ON discovery_runs BEGIN SELECT RAISE(ABORT, 'run storage unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	sender := &countSender{}
	d := New(fakeDocker{cs: []discovery.Container{{ID: "c1", Name: "new"}}}, nil, "", nil, svcList{}, nil, store.NewDiscoveryRunRepo(db), sender, nil, nil)
	if err := d.RunDocker(context.Background()); err == nil || !strings.Contains(err.Error(), "run storage unavailable") {
		t.Fatalf("persistence error was lost: %v", err)
	}
	if sender.count() != 1 {
		t.Fatal("persistence failure suppressed a valid notification")
	}
	scanErr := errors.New("scan unavailable")
	d.docker = fakeDocker{err: scanErr}
	if err := d.RunDocker(context.Background()); !errors.Is(err, scanErr) || !strings.Contains(err.Error(), "run storage unavailable") {
		t.Fatalf("scan and persistence failures must both survive: %v", err)
	}
	if sender.count() != 1 {
		t.Fatal("failed scan must not notify")
	}
}
