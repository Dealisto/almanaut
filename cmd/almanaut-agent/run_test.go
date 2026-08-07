package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/agentapi"
)

// agentEnv writes a config pointing at srv and returns the config path and a
// fresh state dir.
func agentEnv(t *testing.T, serverURL string) (configPath, stateDir string) {
	t.Helper()
	dir := t.TempDir()
	configPath = filepath.Join(dir, "config.toml")
	body := "server_url = \"" + serverURL + "\"\ntoken = \"alm_test\"\n"
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath, filepath.Join(dir, "state")
}

func TestRunReportsAndExitsZero(t *testing.T) {
	var got agentapi.Report
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_ = json.NewEncoder(w).Encode(map[string]any{"host_id": 7, "changed": []string{"ram"}})
	}))
	defer srv.Close()
	cfg, state := agentEnv(t, srv.URL)

	var out, errOut bytes.Buffer
	code := run(context.Background(), []string{"-config", cfg, "-state-dir", state}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, errOut.String())
	}
	if got.AgentID == "" || got.Hostname == "" {
		t.Fatalf("server received an incomplete report: %+v", got)
	}
	if got.SchemaVersion != agentapi.SchemaVersion {
		t.Fatalf("SchemaVersion = %d", got.SchemaVersion)
	}
	// The agent logs what it changed, which is what makes a report debuggable.
	if !strings.Contains(out.String(), "ram") {
		t.Fatalf("stdout %q does not mention the changed field", out.String())
	}
}

// The identity must survive across runs, or every hour would create a new host.
func TestRunReusesTheSameAgentIDAcrossRuns(t *testing.T) {
	var ids []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var rep agentapi.Report
		_ = json.NewDecoder(r.Body).Decode(&rep)
		ids = append(ids, rep.AgentID)
		_ = json.NewEncoder(w).Encode(map[string]any{"host_id": 1})
	}))
	defer srv.Close()
	cfg, state := agentEnv(t, srv.URL)

	for i := 0; i < 2; i++ {
		var out, errOut bytes.Buffer
		if code := run(context.Background(), []string{"-config", cfg, "-state-dir", state}, &out, &errOut); code != 0 {
			t.Fatalf("run %d exit = %d (%s)", i, code, errOut.String())
		}
	}
	if len(ids) != 2 || ids[0] != ids[1] || ids[0] == "" {
		t.Fatalf("agent ids = %v, want the same non-empty id twice", ids)
	}
}

func TestRunExitCodes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		want   int
	}{
		{"conflict needs a human", http.StatusConflict, 4},
		{"bad token will not fix itself", http.StatusUnauthorized, 2},
		{"bad request will not fix itself", http.StatusBadRequest, 2},
		{"server error is transient", http.StatusInternalServerError, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "detail"})
			}))
			defer srv.Close()
			cfg, state := agentEnv(t, srv.URL)
			var out, errOut bytes.Buffer
			if code := run(context.Background(), []string{"-config", cfg, "-state-dir", state}, &out, &errOut); code != tc.want {
				t.Fatalf("exit = %d, want %d (stderr: %s)", code, tc.want, errOut.String())
			}
			if errOut.Len() == 0 {
				t.Fatal("a failing run wrote nothing to stderr")
			}
		})
	}
}

func TestRunMissingConfigExitsOne(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run(context.Background(), []string{"-config", filepath.Join(t.TempDir(), "absent.toml")}, &out, &errOut)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
}

func TestResetIDPrintsANewIdentity(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	var out1, errOut bytes.Buffer
	if code := run(context.Background(), []string{"-state-dir", dir, "reset-id"}, &out1, &errOut); code != 0 {
		t.Fatalf("exit = %d (%s)", code, errOut.String())
	}
	var out2 bytes.Buffer
	if code := run(context.Background(), []string{"-state-dir", dir, "reset-id"}, &out2, &errOut); code != 0 {
		t.Fatalf("second exit = %d", code)
	}
	if out1.String() == out2.String() {
		t.Fatalf("reset-id printed the same id twice: %q", out1.String())
	}
	// reset-id must work with no config at all — the operator runs it on a
	// clone precisely when reporting is broken.
	if strings.Contains(errOut.String(), "config") {
		t.Fatalf("reset-id complained about config: %q", errOut.String())
	}
}

func TestUnknownSubcommandExitsOne(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"wat"}, &out, &errOut); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
}
