package web

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRecovererReturns500AndLogsPanic(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)
	h := recoverer(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if !strings.Contains(buf.String(), "panic: boom") {
		t.Fatalf("log = %q, want it to contain \"panic: boom\"", buf.String())
	}
}

func TestRecovererRepanicsErrAbortHandler(t *testing.T) {
	logger := log.New(&bytes.Buffer{}, "", 0)
	h := recoverer(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(http.ErrAbortHandler)
	}))

	defer func() {
		if rec := recover(); rec != http.ErrAbortHandler {
			t.Fatalf("recovered %v, want http.ErrAbortHandler to propagate", rec)
		}
	}()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}
