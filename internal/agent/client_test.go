package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/agentapi"
)

func testReport() agentapi.Report {
	return agentapi.Report{
		SchemaVersion: agentapi.SchemaVersion,
		AgentID:       "id-1",
		Hostname:      "nas01",
	}
}

func TestSendPostsToTheAgentEndpointWithBearerToken(t *testing.T) {
	var gotPath, gotAuth, gotContentType string
	var gotBody agentapi.Report
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth, gotContentType = r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{"host_id": 42, "changed": []string{"ram"}})
	}))
	defer srv.Close()

	res, err := Send(context.Background(), srv.Client(), srv.URL, "alm_tok", testReport())
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotPath != "/api/agent/report" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotAuth != "Bearer alm_tok" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if !strings.HasPrefix(gotContentType, "application/json") {
		t.Fatalf("Content-Type = %q", gotContentType)
	}
	if gotBody.AgentID != "id-1" {
		t.Fatalf("server received %+v", gotBody)
	}
	if res.HostID != 42 || len(res.Changed) != 1 || res.Changed[0] != "ram" {
		t.Fatalf("result = %+v", res)
	}
}

// A trailing slash on server_url is the likeliest operator typo; it must not
// produce //api/agent/report.
func TestSendNormalizesTrailingSlash(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{"host_id": 1})
	}))
	defer srv.Close()
	if _, err := Send(context.Background(), srv.Client(), srv.URL+"/", "t", testReport()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotPath != "/api/agent/report" {
		t.Fatalf("path = %q", gotPath)
	}
}

// 409 must be distinguishable: it is the only outcome a human has to act on,
// and the server's message carries the reset-id instruction.
func TestSendReturnsErrConflictOn409(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "run 'almanaut-agent reset-id' on the clone"})
	}))
	defer srv.Close()
	_, err := Send(context.Background(), srv.Client(), srv.URL, "t", testReport())
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
	if !strings.Contains(err.Error(), "reset-id") {
		t.Fatalf("err %q does not carry the server's instruction", err)
	}
}

func TestSendReturnsErrRejectedOn4xx(t *testing.T) {
	for _, code := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "nope"})
		}))
		_, err := Send(context.Background(), srv.Client(), srv.URL, "t", testReport())
		srv.Close()
		if !errors.Is(err, ErrRejected) {
			t.Fatalf("status %d gave err = %v, want ErrRejected", code, err)
		}
	}
}

// A 5xx is transient: it must NOT be ErrRejected, because the caller exits
// with a different code for "try again next hour" than for "fix your config".
func TestSendTreats5xxAsTransient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	_, err := Send(context.Background(), srv.Client(), srv.URL, "t", testReport())
	if err == nil {
		t.Fatal("Send on 500 = nil, want error")
	}
	if errors.Is(err, ErrRejected) || errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want a plain transient error", err)
	}
}

func TestSendFailsOnUnreachableServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close() // now nothing is listening
	if _, err := Send(context.Background(), http.DefaultClient, url, "t", testReport()); err == nil {
		t.Fatal("Send to a closed server = nil, want error")
	}
}

// A 200 with no host_id is a misconfigured server (pointing at wrong endpoint).
func TestSendRejectsEmptyHostIDObject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{})
	}))
	defer srv.Close()
	_, err := Send(context.Background(), srv.Client(), srv.URL, "t", testReport())
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("err = %v, want ErrRejected", err)
	}
	if !strings.Contains(err.Error(), "/api/agent/report") {
		t.Fatalf("err %q does not name the endpoint", err)
	}
}

// A 200 with null body is a misconfigured server.
func TestSendRejectsNullHostID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("null"))
	}))
	defer srv.Close()
	_, err := Send(context.Background(), srv.Client(), srv.URL, "t", testReport())
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("err = %v, want ErrRejected", err)
	}
}

// A 200 with explicit 0 host_id is a misconfigured server.
func TestSendRejectsZeroHostID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]int64{"host_id": 0})
	}))
	defer srv.Close()
	_, err := Send(context.Background(), srv.Client(), srv.URL, "t", testReport())
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("err = %v, want ErrRejected", err)
	}
}
