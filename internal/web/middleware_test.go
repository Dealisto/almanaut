package web

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5/middleware"
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

func TestRequestLoggerLogsAndPreservesResponse(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)
	h := requestLogger(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("hi"))
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/hosts", nil))

	if rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want 418 (response must be untouched)", rec.Code)
	}
	if rec.Body.String() != "hi" {
		t.Fatalf("body = %q, want \"hi\"", rec.Body.String())
	}
	line := buf.String()
	for _, want := range []string{"GET", "/hosts", "418"} {
		if !strings.Contains(line, want) {
			t.Fatalf("log line %q missing %q", line, want)
		}
	}
}

func TestRequestLoggerSetsRequestIDHeaderFromContext(t *testing.T) {
	logger := log.New(&bytes.Buffer{}, "", 0)
	// chi's RequestID middleware populates the context; wrap requestLogger inside it.
	h := middleware.RequestID(requestLogger(logger)(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {},
	)))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Header().Get("X-Request-Id") == "" {
		t.Fatal("X-Request-Id response header is empty, want a generated id")
	}
}
