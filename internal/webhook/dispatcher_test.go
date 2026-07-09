package webhook

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
)

type fakeLister struct{ hooks []domain.Webhook }

func (f fakeLister) ListEnabled() ([]domain.Webhook, error) { return f.hooks, nil }

// captured records one received request.
type captured struct {
	sig  string
	body []byte
}

func recordingServer(t *testing.T, status int) (*httptest.Server, *[]captured, *sync.Mutex) {
	t.Helper()
	var mu sync.Mutex
	var got []captured
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		got = append(got, captured{sig: r.Header.Get("X-Almanaut-Signature"), body: b})
		mu.Unlock()
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv, &got, &mu
}

func testQueue(repo Lister, opts Options) *Queue {
	if opts.Logger == nil {
		opts.Logger = log.New(io.Discard, "", 0)
	}
	return NewQueue(repo, opts)
}

func TestQueueDeliversSignedPost(t *testing.T) {
	srv, got, mu := recordingServer(t, http.StatusOK)
	q := testQueue(fakeLister{hooks: []domain.Webhook{
		{URL: srv.URL, Secret: "sek", Enabled: true},
	}}, Options{Workers: 1})

	e, _ := NewEvent("host", 1, ActionCreated, "alice", "2026-07-09T00:00:00Z", map[string]string{"name": "box"})
	q.Dispatch(e)
	q.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(*got) != 1 {
		t.Fatalf("got %d deliveries, want 1", len(*got))
	}
	if (*got)[0].sig == "" {
		t.Errorf("missing signature header")
	}
}

func TestQueueHonoursFilter(t *testing.T) {
	srv, got, mu := recordingServer(t, http.StatusOK)
	q := testQueue(fakeLister{hooks: []domain.Webhook{
		{URL: srv.URL, Secret: "s", Enabled: true, EntityTypes: []string{"service"}},
	}}, Options{Workers: 1})

	e, _ := NewEvent("host", 1, ActionCreated, "a", "t", map[string]string{})
	q.Dispatch(e)
	q.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(*got) != 0 {
		t.Fatalf("got %d deliveries, want 0 (filtered out)", len(*got))
	}
}

func TestQueueRetriesThenGivesUp(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	q := testQueue(fakeLister{hooks: []domain.Webhook{{URL: srv.URL, Secret: "s", Enabled: true}}},
		Options{Workers: 1, MaxAttempts: 3, BaseDelay: time.Millisecond})

	e, _ := NewEvent("host", 1, ActionCreated, "a", "t", map[string]string{})
	q.Dispatch(e)
	q.Wait()

	if n := atomic.LoadInt32(&attempts); n != 3 {
		t.Fatalf("attempts = %d, want 3", n)
	}
}

func TestQueueOneFailingEndpointDoesNotBlockSibling(t *testing.T) {
	okSrv, okGot, okMu := recordingServer(t, http.StatusOK)
	var failHits int32
	failSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&failHits, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(failSrv.Close)

	q := testQueue(fakeLister{hooks: []domain.Webhook{
		{URL: failSrv.URL, Secret: "s", Enabled: true},
		{URL: okSrv.URL, Secret: "s", Enabled: true},
	}}, Options{Workers: 1, MaxAttempts: 3, BaseDelay: time.Millisecond})

	e, _ := NewEvent("host", 1, ActionCreated, "a", "t", map[string]string{})
	q.Dispatch(e)
	q.Wait()

	okMu.Lock()
	defer okMu.Unlock()
	if len(*okGot) != 1 {
		t.Fatalf("healthy endpoint got %d deliveries, want 1", len(*okGot))
	}
	if n := atomic.LoadInt32(&failHits); n != 3 {
		t.Fatalf("failing endpoint attempts = %d, want 3", n)
	}
}

type recordingDispatcher struct{ got []Event }

func (r *recordingDispatcher) Dispatch(events ...Event) { r.got = append(r.got, events...) }

func TestMultiFansOut(t *testing.T) {
	a, b := &recordingDispatcher{}, &recordingDispatcher{}
	m := Multi{a, b}
	m.Dispatch(Event{Type: "service", ID: 1}, Event{Type: "host", ID: 2})
	if len(a.got) != 2 || len(b.got) != 2 {
		t.Fatalf("fan-out incomplete: a=%d b=%d", len(a.got), len(b.got))
	}
}
