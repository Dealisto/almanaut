package job

import (
	"context"
	"errors"
	"io"
	"log"
	"sync"
	"testing"
	"time"
)

func noop(context.Context) error { return nil }

// signaling returns a RunFunc that signals each pass on ch and returns
// errs[n] for the n-th pass (nil once n exceeds len(errs)).
func signaling(ch chan<- int, errs ...error) RunFunc {
	var n int
	var mu sync.Mutex
	return func(ctx context.Context) error {
		mu.Lock()
		i := n
		n++
		mu.Unlock()
		ch <- i
		if i < len(errs) {
			return errs[i]
		}
		return nil
	}
}

func waitFor(t *testing.T, ch <-chan int) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for a job pass")
	}
}

func discardLogger() *log.Logger { return log.New(io.Discard, "", 0) }

func statusOf(r *Runner, name string) Status {
	for _, s := range r.Statuses() {
		if s.Name == name {
			return s
		}
	}
	return Status{}
}

func eventually(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("condition not met within 2s")
}

func TestStartRunsImmediatePass(t *testing.T) {
	ch := make(chan int, 4)
	r := New(discardLogger())
	r.Register(Definition{Name: "a", Interval: time.Hour, Run: signaling(ch)})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.Start(ctx)

	waitFor(t, ch) // immediate startup pass, no need to wait for the hour tick
}

func TestTriggerCausesExtraPass(t *testing.T) {
	ch := make(chan int, 4)
	r := New(discardLogger())
	r.Register(Definition{Name: "a", Interval: time.Hour, Run: signaling(ch)})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.Start(ctx)

	waitFor(t, ch) // startup pass
	if !r.Trigger("a") {
		t.Fatal("Trigger(a) = false, want true")
	}
	waitFor(t, ch) // triggered pass

	if r.Trigger("nope") {
		t.Fatal("Trigger(unknown) = true, want false")
	}
}

func TestErrorIsCapturedThenCleared(t *testing.T) {
	ch := make(chan int, 4)
	r := New(discardLogger())
	r.Register(Definition{Name: "a", Interval: time.Hour,
		Run: signaling(ch, errors.New("boom"))}) // pass 0 errors, pass 1 ok
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.Start(ctx)

	waitFor(t, ch) // failing startup pass
	eventually(t, func() bool { return statusOf(r, "a").LastErr == "boom" })

	r.Trigger("a")
	waitFor(t, ch) // succeeding pass
	eventually(t, func() bool {
		s := statusOf(r, "a")
		return s.LastErr == "" && s.Runs == 2
	})
}

func TestTimeoutCancelsPass(t *testing.T) {
	ch := make(chan int, 2)
	r := New(discardLogger())
	r.Register(Definition{Name: "a", Interval: time.Hour, Timeout: 20 * time.Millisecond,
		Run: func(ctx context.Context) error {
			ch <- 0
			<-ctx.Done() // block until the per-pass timeout fires
			return ctx.Err()
		}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.Start(ctx)

	waitFor(t, ch)
	eventually(t, func() bool { return statusOf(r, "a").LastErr != "" })
}

func TestTriggerOnlyJobHasNoNextRun(t *testing.T) {
	ch := make(chan int, 4)
	r := New(discardLogger())
	r.Register(Definition{Name: "a", Interval: 0, Run: signaling(ch)})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.Start(ctx)

	waitFor(t, ch) // startup pass
	r.Trigger("a")
	waitFor(t, ch) // triggered pass

	eventually(t, func() bool { return statusOf(r, "a").Runs >= 2 })
	s := statusOf(r, "a")
	if !s.NextRun.IsZero() {
		t.Fatalf("NextRun = %v, want zero (trigger-only job should never be scheduled)", s.NextRun)
	}
}

func TestStatusesReturnsRegisteredJobsInOrder(t *testing.T) {
	r := New(nil)
	r.Register(Definition{Name: "a", Title: "Job A", Interval: time.Hour, Run: noop})
	r.Register(Definition{Name: "b", Title: "Job B", Interval: 2 * time.Hour, Run: noop})

	got := r.Statuses()
	if len(got) != 2 {
		t.Fatalf("Statuses len = %d, want 2", len(got))
	}
	if got[0].Name != "a" || got[1].Name != "b" {
		t.Fatalf("order = %q,%q; want a,b", got[0].Name, got[1].Name)
	}
	if got[0].Title != "Job A" || got[0].Interval != time.Hour {
		t.Fatalf("job a snapshot wrong: %+v", got[0])
	}
	// Never-run job has zero LastRun and empty LastErr.
	if !got[0].LastRun.IsZero() || got[0].LastErr != "" || got[0].Runs != 0 {
		t.Fatalf("fresh job should be zero-valued: %+v", got[0])
	}
}
