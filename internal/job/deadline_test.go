package job

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPassRecordsCancellationWhenCallbackReturnsNil(t *testing.T) {
	for _, tc := range []struct {
		name            string
		parentCancelled bool
		want            error
	}{
		{"deadline", false, context.DeadlineExceeded},
		{"shutdown", true, context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			timeout := 5 * time.Millisecond
			if tc.parentCancelled {
				timeout = time.Minute
			}
			r := New(discardLogger())
			r.Register(Definition{Name: "a", Timeout: timeout, Run: func(ctx context.Context) error {
				if tc.parentCancelled {
					cancel()
				}
				<-ctx.Done()
				return nil
			}})
			r.pass(ctx, r.jobs[0])
			if got := statusOf(r, "a"); got.LastErr != tc.want.Error() || got.Running || got.Runs != 1 {
				t.Fatalf("interrupted pass reported as successful: %+v", got)
			}
		})
	}
}

func TestPassPreservesCallbackErrorAfterDeadline(t *testing.T) {
	r := New(discardLogger())
	r.Register(Definition{Name: "a", Timeout: 5 * time.Millisecond, Run: func(ctx context.Context) error {
		<-ctx.Done()
		return errors.New("delivery interrupted")
	}})
	r.pass(context.Background(), r.jobs[0])
	if got := statusOf(r, "a").LastErr; got != "delivery interrupted" {
		t.Fatalf("callback diagnostic lost: %q", got)
	}
}
