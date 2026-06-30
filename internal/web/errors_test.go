package web

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestServerError verifies that serverError sends a generic 500 to the client
// while logging the real error (with method and path) server-side, so internal
// detail never reaches the response body.
func TestServerError(t *testing.T) {
	var buf bytes.Buffer
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/things/5", nil)
	r = r.WithContext(context.WithValue(r.Context(), loggerCtxKey{}, log.New(&buf, "", 0)))

	serverError(rec, r, errors.New("sensitive db detail: /var/lib/x"))

	if rec.Code != 500 {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if body := rec.Body.String(); body != "internal server error\n" {
		t.Errorf("body = %q, want %q", body, "internal server error\n")
	}
	if strings.Contains(rec.Body.String(), "sensitive db detail") {
		t.Errorf("response body leaked internal detail: %q", rec.Body.String())
	}
	logged := buf.String()
	if !strings.Contains(logged, "sensitive db detail") {
		t.Errorf("log %q does not contain the internal error detail", logged)
	}
	if !strings.Contains(logged, "GET") {
		t.Errorf("log %q does not contain the request method", logged)
	}
	if !strings.Contains(logged, "/things/5") {
		t.Errorf("log %q does not contain the request path", logged)
	}
}
