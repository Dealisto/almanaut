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
	"sync/atomic"
	"testing"

	"github.com/Dealisto/almanaut/internal/agent"
	"github.com/Dealisto/almanaut/internal/agentapi"
)

func TestRunHelpExitsSuccessfully(t *testing.T) {
	for _, flag := range []string{"-h", "--help"} {
		var out, errOut bytes.Buffer
		if code := run(context.Background(), []string{flag}, &out, &errOut); code != exitOK || !strings.Contains(errOut.String(), "usage:") {
			t.Fatalf("%s: code=%d stderr=%s", flag, code, errOut.String())
		}
	}
}

func TestRunRejectsExtraArgumentsBeforeSideEffects(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"host_id": 1})
	}))
	defer srv.Close()
	cfg, state := agentEnv(t, srv.URL)
	id, err := agent.LoadOrCreateAgentID(state)
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"report", "typo"},
		{"report", "-timeout", "1s"},
		{"reset-id", "--help"},
		{"reset-id", "-state-dir", filepath.Join(t.TempDir(), "other")},
		{"version", "extra"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var out, errOut bytes.Buffer
			fullArgs := append([]string{"-config", cfg, "-state-dir", state}, args...)
			if code := run(context.Background(), fullArgs, &out, &errOut); code != exitUsage {
				t.Fatalf("code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
			}
			if out.Len() != 0 || !strings.Contains(errOut.String(), "flags must precede the command") {
				t.Fatalf("stdout=%s stderr=%s", out.String(), errOut.String())
			}
			if got, err := agent.LoadOrCreateAgentID(state); err != nil || got != id {
				t.Fatalf("invalid arguments changed identity: got=%q err=%v", got, err)
			}
		})
	}
	if requests.Load() != 0 {
		t.Fatalf("invalid arguments sent %d reports", requests.Load())
	}
}

func TestRunRejectsNonPositiveTimeout(t *testing.T) {
	for _, timeout := range []string{"0s", "-1s"} {
		var out, errOut bytes.Buffer
		if code := run(context.Background(), []string{"-timeout", timeout}, &out, &errOut); code != exitUsage || !strings.Contains(errOut.String(), "timeout must be positive") {
			t.Fatalf("timeout %s: code=%d stderr=%s", timeout, code, errOut.String())
		}
	}
}

func TestRunCanceledCollectionNeverSendsReport(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
	}))
	defer srv.Close()
	cfg, state := agentEnv(t, srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out, errOut bytes.Buffer
	code := run(ctx, []string{"-config", cfg, "-state-dir", state}, &out, &errOut)
	if code != exitUnreachable || requests.Load() != 0 || !strings.Contains(errOut.String(), "collect host facts") {
		t.Fatalf("canceled report: code=%d requests=%d stderr=%s", code, requests.Load(), errOut.String())
	}
}

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

func TestRunInvalidEndpointDoesNotCreateIdentity(t *testing.T) {
	cfg, state := agentEnv(t, "http://localhost:65536")
	var out, errOut bytes.Buffer
	code := run(context.Background(), []string{"-config", cfg, "-state-dir", state}, &out, &errOut)
	if code != exitUsage || !strings.Contains(errOut.String(), "port must be") {
		t.Fatalf("invalid endpoint: code=%d stderr=%s", code, errOut.String())
	}
	if _, err := os.Stat(state); !os.IsNotExist(err) {
		t.Fatalf("invalid configuration created state: %v", err)
	}
}

func TestRunMalformedSuccessIsPermanentRejection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html>Sign in</html>"))
	}))
	defer srv.Close()
	cfg, state := agentEnv(t, srv.URL)
	var out, errOut bytes.Buffer
	code := run(context.Background(), []string{"-config", cfg, "-state-dir", state}, &out, &errOut)
	if code != exitRejected || !strings.Contains(errOut.String(), "invalid JSON response") {
		t.Fatalf("malformed success: code=%d stderr=%s", code, errOut.String())
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
