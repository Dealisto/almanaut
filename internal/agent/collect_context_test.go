package agent

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestCollectContextReturnsWhileSystemCallIsBlocked(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	opts := CollectOptions{
		Root: fixtureRoot(t, map[string]string{}), VirtKind: "lxc",
		Interfaces: fakeLister(),
		Usage: func(string) (int64, int64, error) {
			calls.Add(1)
			close(entered)
			<-release
			return 100, 20, nil
		},
	}
	t.Cleanup(func() {
		close(release)
		// Wait until the abandoned worker releases its slot before another
		// test starts using collection. No background work outlives this test.
		select {
		case collectionSlot <- struct{}{}:
			<-collectionSlot
		case <-time.After(5 * time.Second):
			t.Error("collection worker failed to finish after the syscall returned")
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		report, err := CollectContext(ctx, opts)
		if report.SchemaVersion != 0 {
			t.Error("canceled collection returned a report that could be sent")
		}
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("collection did not reach the injected syscall")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("collection error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("collection did not return when canceled")
	}
	// A retry must not leak another worker while the first OS call is stuck.
	retryCtx, retryCancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer retryCancel()
	if _, err := CollectContext(retryCtx, opts); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("queued collection error = %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("started %d collection workers while one is blocked", calls.Load())
	}
}

func TestCollectContextReturnsCompleteReport(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	report, err := CollectContext(ctx, CollectOptions{
		Root: fixtureRoot(t, map[string]string{}), VirtKind: "physical",
		AgentID: "id", Hostname: "nas", Interfaces: fakeLister(),
	})
	if err != nil || report.Hostname != "nas" || report.AgentID != "id" {
		t.Fatalf("collection = %+v, %v", report, err)
	}
}
