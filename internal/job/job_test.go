package job

import (
	"context"
	"testing"
	"time"
)

func noop(context.Context) error { return nil }

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
