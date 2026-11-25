package webhook

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
)

func TestQueueShutdownCancelsRequestsAndRejectsNewEvents(t *testing.T) {
	started, canceled := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		close(started)
		<-r.Context().Done()
		close(canceled)
	}))
	t.Cleanup(server.Close)
	q := testQueue(t, fakeLister{hooks: []domain.Webhook{{URL: server.URL, Secret: "secret", Enabled: true}}}, Options{Workers: 1, Timeout: time.Hour})
	e := Event{Type: "host", ID: 1, Action: ActionCreated}
	q.Dispatch(e)
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := q.Shutdown(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Shutdown = %v, want context.Canceled", err)
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not cancel the HTTP request")
	}
	cleanupCtx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if err := q.Shutdown(cleanupCtx); err != nil {
		t.Fatalf("shutdown cleanup: %v", err)
	}
	q.Dispatch(e)
	q.Wait()
	if calls.Load() != 1 || len(q.slots) != 0 {
		t.Fatalf("shutdown left work: calls=%d slots=%d", calls.Load(), len(q.slots))
	}
}

func TestQueueShutdownDrainsAcceptedDeliveries(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	q := testQueue(t, fakeLister{hooks: []domain.Webhook{{URL: server.URL, Secret: "secret", Enabled: true}}}, Options{Workers: 1})
	q.Dispatch(Event{Type: "host", ID: 1, Action: ActionCreated})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := q.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("accepted delivery was dropped: calls=%d", calls.Load())
	}
}

func waitForRetries(t *testing.T, q *Queue, count int) {
	t.Helper()
	timeout := time.NewTimer(time.Second)
	defer timeout.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		q.retryMu.Lock()
		n := len(q.retries)
		q.retryMu.Unlock()
		if n == count {
			return
		}
		select {
		case <-timeout.C:
			t.Fatalf("pending retries = %d, want %d", n, count)
		case <-tick.C:
		}
	}
}

type listerFunc func() ([]domain.Webhook, error)

func (f listerFunc) ListEnabled() ([]domain.Webhook, error) { return f() }

func TestQueueBoundsDelayedDeliveriesAndCancelsRetryTimers(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)
	expanded := make(chan struct{}, 1)
	repo := listerFunc(func() ([]domain.Webhook, error) {
		expanded <- struct{}{}
		return []domain.Webhook{{URL: server.URL, Secret: "secret", Enabled: true}}, nil
	})
	q := testQueue(t, repo, Options{Workers: 1, QueueSize: 1, BaseDelay: time.Hour})
	e := Event{Type: "host", ID: 1, Action: ActionCreated}
	// Fill the shared capacity with delayed attempts while both channels are
	// empty. New events must not create unbounded timers retaining payloads.
	for n := 1; n <= 2; n++ {
		q.Dispatch(e)
		<-expanded
		waitForRetries(t, q, n)
	}
	for range 100 {
		q.Dispatch(e)
		<-expanded
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = q.Shutdown(ctx)
	cleanupCtx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if err := q.Shutdown(cleanupCtx); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || len(q.slots) != 0 {
		t.Fatalf("delivery bound/cleanup failed: calls=%d slots=%d", calls.Load(), len(q.slots))
	}
	q.retryMu.Lock()
	defer q.retryMu.Unlock()
	if len(q.retries) != 0 {
		t.Fatalf("shutdown left %d timers", len(q.retries))
	}
}

func TestQueueConcurrentDispatchAndShutdown(t *testing.T) {
	q := testQueue(t, fakeLister{}, Options{Workers: 2, QueueSize: 4})
	var producers sync.WaitGroup
	for range 8 {
		producers.Go(func() {
			for range 100 {
				q.Dispatch(Event{Type: "host", ID: 1, Action: ActionCreated})
			}
		})
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := q.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	producers.Wait()
}

func TestQueueAppliesDeliveryPolicyWithoutMutatingInjectedClient(t *testing.T) {
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		redirected.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(target.Close)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(server.Close)
	client := server.Client()
	q := testQueue(t, fakeLister{hooks: []domain.Webhook{{URL: server.URL, Secret: "secret", Enabled: true}}}, Options{
		Workers: 1, MaxAttempts: 1, Timeout: time.Second, Client: client,
	})
	q.Dispatch(Event{Type: "host", ID: 1, Action: ActionCreated})
	q.Wait()
	if redirected.Load() != 0 {
		t.Fatal("injected client replayed the signed delivery at a redirect target")
	}
	if client.Timeout != 0 || client.CheckRedirect != nil {
		t.Fatal("queue mutated the caller's shared HTTP client")
	}
	if q.client.Timeout != time.Second {
		t.Fatal("injected client without a timeout did not receive the delivery bound")
	}
}

func TestRetryDelayDoesNotOverflow(t *testing.T) {
	for _, tc := range []struct {
		base    time.Duration
		attempt int
		want    time.Duration
	}{
		{2 * time.Second, 1, 2 * time.Second},
		{2 * time.Second, 5, 32 * time.Second},
		{time.Duration(1 << 62), 2, time.Duration(1<<63 - 1)},
		{time.Nanosecond, 1000, time.Duration(1<<63 - 1)},
	} {
		if got := retryDelay(tc.base, tc.attempt); got != tc.want {
			t.Errorf("retryDelay(%v, %d) = %v, want %v", tc.base, tc.attempt, got, tc.want)
		}
	}
}
