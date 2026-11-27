package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunTreatsIncompleteErrorResponseAsTransient(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusConflict} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				w.Header().Set("Content-Length", "1024")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"error":"interrupted`)
			}))
			defer srv.Close()
			cfg, state := agentEnv(t, srv.URL)
			var out, errOut bytes.Buffer
			code := run(context.Background(), []string{"-config", cfg, "-state-dir", state}, &out, &errOut)
			if code != exitUnreachable || !strings.Contains(errOut.String(), "unexpected EOF") {
				t.Fatalf("incomplete error response: code=%d stderr=%s", code, errOut.String())
			}
		})
	}
}
