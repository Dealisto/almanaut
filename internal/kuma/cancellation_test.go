package kuma

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCanceledOperationsLeaveSessionUsableAndMonitorsUnchanged(t *testing.T) {
	for _, operation := range []string{"add", "edit", "delete"} {
		for _, deadline := range []bool{false, true} {
			name := operation + "/canceled"
			if deadline {
				name = operation + "/deadline"
			}
			t.Run(name, func(t *testing.T) {
				f := newFakeKuma(t)
				id := f.putMonitor("original", "https://original.example")
				s, err := NewClient(f.url(), f.user, f.pass, false).Connect(testCtx(t))
				if err != nil {
					t.Fatal(err)
				}
				defer s.Close()
				ctx, cancel := context.WithCancel(testCtx(t))
				want := context.Canceled
				if deadline {
					cancel()
					ctx, cancel = context.WithDeadline(testCtx(t), time.Now().Add(-time.Second))
					want = context.DeadlineExceeded
				}
				cancel()
				monitor := Monitor{ID: id, Name: "changed", URL: "https://changed.example"}
				switch operation {
				case "add":
					_, err = s.Add(ctx, monitor)
				case "edit":
					err = s.Edit(ctx, monitor)
				case "delete":
					err = s.Delete(ctx, id)
				}
				if !errors.Is(err, want) {
					t.Fatalf("operation lost cancellation: got %v, want %v", err, want)
				}
				// The successful operation also fences earlier socket writes, so a
				// mistakenly transmitted mutation cannot arrive after the assertions.
				added, err := s.Add(testCtx(t), Monitor{Name: "healthy", URL: "https://healthy.example"})
				if err != nil {
					t.Fatalf("canceled operation poisoned the session: %v", err)
				}
				if added != id+1 || f.monitorCount() != 2 {
					t.Fatalf("canceled operation changed inventory: added=%d count=%d", added, f.monitorCount())
				}
				original, ok := f.getMonitor(id)
				if !ok || original["name"] != "original" || original["url"] != "https://original.example" {
					t.Fatalf("canceled operation mutated the original monitor: %v", original)
				}
			})
		}
	}
}
