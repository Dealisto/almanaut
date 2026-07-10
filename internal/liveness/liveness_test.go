package liveness

import (
	"context"
	"errors"
	"strconv"
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
func key(t string, id int64) string { return t + ":" + strconv.FormatInt(id, 10) }

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

	// Mutable clock: advanced between Runs so changed_at/checked_at semantics
	// can be asserted precisely instead of against a single frozen instant.
	cur := time.Now()
	clock := func() time.Time { return cur }

	// first observation: up, records state, no notification
	c := New(hosts, noServices{}, state, sender, up, time.Second, nil, clock)
	if err := c.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if sender.count() != 0 {
		t.Fatalf("first observation must not notify, got %d", sender.count())
	}
	got, _ := state.Get("host", 1)
	firstChangedAt := got.ChangedAt
	if !got.ChangedAt.Equal(cur) || !got.CheckedAt.Equal(cur) {
		t.Fatalf("first observation should stamp changed_at/checked_at to now: %+v", got)
	}

	// up -> up: silent; checked_at advances but changed_at must not move.
	cur = cur.Add(time.Minute)
	if err := c.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if sender.count() != 0 {
		t.Fatalf("up->up must be silent, got %d", sender.count())
	}
	got, _ = state.Get("host", 1)
	if !got.ChangedAt.Equal(firstChangedAt) {
		t.Fatalf("up->up must not move changed_at: got %v want %v", got.ChangedAt, firstChangedAt)
	}
	if !got.CheckedAt.Equal(cur) {
		t.Fatalf("checked_at should advance on every run: got %v want %v", got.CheckedAt, cur)
	}

	// up -> down: one notification tagged "warning"; changed_at advances;
	// last_error equals the dialer's exact error string.
	cur = cur.Add(time.Minute)
	downChangedAt := cur
	c = New(hosts, noServices{}, state, sender, down, time.Second, nil, clock)
	if err := c.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if sender.count() != 1 {
		t.Fatalf("want 1 notification on up->down, got %d", sender.count())
	}
	if sender.notes[0].Tags != "warning" {
		t.Fatalf("down notification must carry the warning tag, got %q", sender.notes[0].Tags)
	}
	got, _ = state.Get("host", 1)
	if got.Status != domain.LivenessDown {
		t.Fatalf("want down status, got %q", got.Status)
	}
	if got.LastError != "connection refused" {
		t.Fatalf("want exact dial error as last_error, got %q", got.LastError)
	}
	if !got.ChangedAt.Equal(downChangedAt) {
		t.Fatalf("up->down must move changed_at to now: got %v want %v", got.ChangedAt, downChangedAt)
	}
	if !got.CheckedAt.Equal(downChangedAt) {
		t.Fatalf("checked_at should advance: got %v want %v", got.CheckedAt, downChangedAt)
	}

	// down -> down: no new notification; changed_at unchanged; checked_at advances.
	cur = cur.Add(time.Minute)
	if err := c.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if sender.count() != 1 {
		t.Fatalf("down->down must be silent, got %d", sender.count())
	}
	got, _ = state.Get("host", 1)
	if got.Status != domain.LivenessDown || got.LastError == "" {
		t.Fatalf("down state/last_error not recorded: %+v", got)
	}
	if !got.ChangedAt.Equal(downChangedAt) {
		t.Fatalf("down->down must not move changed_at: got %v want %v", got.ChangedAt, downChangedAt)
	}
	if !got.CheckedAt.Equal(cur) {
		t.Fatalf("checked_at should advance on every run: got %v want %v", got.CheckedAt, cur)
	}

	// down -> up: recovery notification with empty Tags; changed_at advances again.
	cur = cur.Add(time.Minute)
	upChangedAt := cur
	c = New(hosts, noServices{}, state, sender, up, time.Second, nil, clock)
	if err := c.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if sender.count() != 2 {
		t.Fatalf("want a 2nd notification on down->up recovery, got %d", sender.count())
	}
	if sender.notes[0].Tags != "warning" {
		t.Fatalf("down notification tag regressed, got %q", sender.notes[0].Tags)
	}
	if sender.notes[1].Tags != "" {
		t.Fatalf("recovery notification must not carry the warning tag, got %q", sender.notes[1].Tags)
	}
	got, _ = state.Get("host", 1)
	if got.Status != domain.LivenessUp {
		t.Fatalf("want up status after recovery, got %q", got.Status)
	}
	if !got.ChangedAt.Equal(upChangedAt) {
		t.Fatalf("down->up must move changed_at to now: got %v want %v", got.ChangedAt, upChangedAt)
	}
	if !got.CheckedAt.Equal(upChangedAt) {
		t.Fatalf("checked_at should advance: got %v want %v", got.CheckedAt, upChangedAt)
	}
}

// TestCheckerSkipsAlreadyCancelledContext guards against a cancelled context
// (e.g. mid-shutdown) recording a spurious "down" transition and firing a
// false DOWN notification. The dialer below always reports unreachable, so
// without the ctx guard this would flip an "up" entity to "down" and notify.
func TestCheckerSkipsAlreadyCancelledContext(t *testing.T) {
	state := newFakeState()
	sender := &countingSender{}
	up := func(context.Context, string) error { return nil }
	down := func(context.Context, string) error { return errors.New("connection refused") }

	hosts := hostList{{ID: 1, Name: "web", CheckAddress: "x:1"}}
	now := time.Now()
	clock := func() time.Time { return now }

	// Establish a prior "up" observation.
	c := New(hosts, noServices{}, state, sender, up, time.Second, nil, clock)
	if err := c.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	before, _ := state.Get("host", 1)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c2 := New(hosts, noServices{}, state, sender, down, time.Second, nil, clock)
	_ = c2.Run(ctx)

	if sender.count() != 0 {
		t.Fatalf("cancelled context must not notify, got %d", sender.count())
	}
	after, _ := state.Get("host", 1)
	if after != before {
		t.Fatalf("cancelled context must not change state: before %+v after %+v", before, after)
	}
}

// TestCheckerSkipsWhenParentContextEndsMidDial guards against the parent
// context (job Timeout deadline, or shutdown) ending while a dial is in
// flight. Because dialCtx is derived from the parent via
// context.WithTimeout(ctx, c.timeout), a dial that returns because the parent
// ended looks identical to a real connection failure unless check() checks
// the parent ctx again after the dial. Without that guard this would record a
// spurious "down" for a prior-"up" entity and fire a false DOWN notification.
func TestCheckerSkipsWhenParentContextEndsMidDial(t *testing.T) {
	state := newFakeState()
	sender := &countingSender{}
	up := func(context.Context, string) error { return nil }

	hosts := hostList{{ID: 1, Name: "web", CheckAddress: "x:1"}}
	now := time.Now()
	clock := func() time.Time { return now }

	// Establish a prior "up" observation.
	c := New(hosts, noServices{}, state, sender, up, time.Second, nil, clock)
	if err := c.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	before, _ := state.Get("host", 1)

	ctx, cancel := context.WithCancel(context.Background())
	dial := func(dialCtx context.Context, addr string) error {
		cancel()             // parent ends while "dialing"
		return dialCtx.Err() // dialCtx derives from parent -> now non-nil
	}

	c2 := New(hosts, noServices{}, state, sender, dial, time.Second, nil, clock)
	_ = c2.Run(ctx)

	if sender.count() != 0 {
		t.Fatalf("parent context ending mid-dial must not notify, got %d", sender.count())
	}
	after, _ := state.Get("host", 1)
	if after != before {
		t.Fatalf("parent context ending mid-dial must not change state: before %+v after %+v", before, after)
	}
}
