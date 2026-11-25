package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
)

func TestPasswordWorkBoundsConcurrentRequestsAndRecoversCapacity(t *testing.T) {
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var calls atomic.Int32
	h := passwordWorkLimit(2)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		done, ok := acquirePasswordWork(w, r)
		if !ok {
			return
		}
		defer done()
		calls.Add(1)
		entered <- struct{}{}
		<-release
		w.WriteHeader(http.StatusNoContent)
	}))
	var workers sync.WaitGroup
	for range 2 {
		workers.Go(func() { h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/login", nil)) })
	}
	<-entered
	<-entered
	for range 20 {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("POST", "/login", nil))
		if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") == "" {
			t.Fatalf("overloaded admission: status=%d headers=%v", rec.Code, rec.Header())
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("password workers = %d, want 2", calls.Load())
	}
	close(release)
	workers.Wait()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/login", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("capacity not released: %d", rec.Code)
	}
}

func TestPasswordWorkSkipsCanceledRequestsAndReleasesAfterPanic(t *testing.T) {
	var reached bool
	h := passwordWorkLimit(1)(recoverer(testLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		done, ok := acquirePasswordWork(w, r)
		if !ok {
			return
		}
		defer done()
		reached = true
		panic("worker failed")
	})))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/login", nil).WithContext(ctx))
	if reached || rec.Code != http.StatusRequestTimeout {
		t.Fatalf("canceled request admitted: reached=%v status=%d", reached, rec.Code)
	}
	for range 2 {
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("POST", "/login", nil))
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("panic leaked a slot: %d", rec.Code)
		}
	}
}

func TestPasswordHandlersShareOneAdmissionPool(t *testing.T) {
	// Saturate the same pool passed through each handler's request context.
	// Nil repositories prove the rejected request cannot reach database/bcrypt
	// work, for a real or arbitrary username alike.
	for name, handler := range map[string]http.HandlerFunc{
		"login":           login(nil, nil, nil, nil, nil, nil, false, false),
		"create user":     createUser(nil),
		"reset password":  resetUserPassword(nil, nil, nil, nil),
		"change password": changePassword(nil, nil, nil, nil),
	} {
		t.Run(name, func(t *testing.T) {
			h := passwordWorkLimit(1)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				done, ok := acquirePasswordWork(w, r)
				if !ok {
					t.Fatal("initial acquisition failed")
				}
				defer done()
				handler.ServeHTTP(w, r)
			}))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest("POST", "/", nil))
			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("status=%d", rec.Code)
			}
		})
	}
}
