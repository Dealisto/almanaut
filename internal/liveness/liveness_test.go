package liveness

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/notify"
	"github.com/Dealisto/almanaut/internal/store"
)

type fakeState struct {
	mu   sync.Mutex
	rows map[string]domain.LivenessStatus
}

func newFakeState() *fakeState      { return &fakeState{rows: map[string]domain.LivenessStatus{}} }
func key(t string, id int64) string { return t + ":" + string(rune(id)) }

func (f *fakeState) Get(t string, id int64) (domain.LivenessStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.rows[key(t, id)]
	if !ok {
		return domain.LivenessStatus{}, store.ErrNotFound
	}
	return s, nil
}
func (f *fakeState) Upsert(t string, id int64, s domain.LivenessStatus) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows[key(t, id)] = s
	return nil
}

type countingSender struct {
	mu    sync.Mutex
	notes []notify.Notification
}

func (c *countingSender) Send(_ context.Context, n notify.Notification) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.notes = append(c.notes, n)
	return nil
}
func (c *countingSender) count() int { c.mu.Lock(); defer c.mu.Unlock(); return len(c.notes) }

type hostList []domain.Host

func (h hostList) List() ([]domain.Host, error) { return []domain.Host(h), nil }

type noServices struct{}

func (noServices) List() ([]domain.Service, error) { return nil, nil }

func TestCheckerTransitionsNotifyOnce(t *testing.T) {
	state := newFakeState()
	sender := &countingSender{}
	up := func(context.Context, string) error { return nil }
	down := func(context.Context, string) error { return errors.New("connection refused") }

	hosts := hostList{{ID: 1, Name: "web", CheckAddress: "x:1"}}
	now := time.Now()
	clock := func() time.Time { return now }

	// first observation: up, records state, no notification
	c := New(hosts, noServices{}, state, sender, up, time.Second, nil, clock)
	if err := c.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if sender.count() != 0 {
		t.Fatalf("first observation must not notify, got %d", sender.count())
	}

	// up -> down: one notification
	c = New(hosts, noServices{}, state, sender, down, time.Second, nil, clock)
	_ = c.Run(context.Background())
	if sender.count() != 1 {
		t.Fatalf("want 1 notification on up->down, got %d", sender.count())
	}

	// down -> down: no new notification
	_ = c.Run(context.Background())
	if sender.count() != 1 {
		t.Fatalf("down->down must be silent, got %d", sender.count())
	}
	got, _ := state.Get("host", 1)
	if got.Status != domain.LivenessDown || got.LastError == "" {
		t.Fatalf("down state/last_error not recorded: %+v", got)
	}
}
