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

type hostList []domain.Host

func (h hostList) List() ([]domain.Host, error) { return []domain.Host(h), nil }

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
