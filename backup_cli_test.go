package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/store"
)

func TestBackupCLIExplicitPathsAndVerification(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.db")
	destination := filepath.Join(dir, "backup.db")
	db, err := store.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := store.Migrate(db, ""); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	args := []string{"backup", "--source", source, "--destination", destination}
	if status := runBackupCommand(args, &stdout, &stderr); status != 0 {
		t.Fatalf("backup status %d: %s", status, stderr.String())
	}
	if !strings.Contains(stdout.String(), "External configuration and secret files are not included") {
		t.Fatal("backup output did not explain external configuration exclusion")
	}
	stdout.Reset()
	stderr.Reset()
	if status := runBackupCommand([]string{"backup-verify", "--source", destination}, &stdout, &stderr); status != 0 {
		t.Fatalf("verification status %d: %s", status, stderr.String())
	}
	if !strings.Contains(stdout.String(), "does not prove restore compatibility") {
		t.Fatal("verification output overpromises restore compatibility")
	}
	stdout.Reset()
	stderr.Reset()
	if status := runBackupCommand(args, &stdout, &stderr); status != 1 {
		t.Fatalf("repeated destination should fail: %d: %s", status, stderr.String())
	}
}

func TestBackupCLIRejectsMissingPathsAndInvalidArgumentsWithoutStartup(t *testing.T) {
	t.Setenv("ALMANAUT_DATA_DIR", filepath.Join(t.TempDir(), "must-not-be-created"))
	t.Setenv("ALMANAUT_AUTH_PASS_FILE", filepath.Join(t.TempDir(), "missing-secret"))
	for _, args := range [][]string{
		{}, {"unknown"}, {"backup"}, {"backup-verify"},
		{"backup", "--source", "missing.db"},
		{"backup-verify", "--source", "missing.db", "--timeout", "0"},
		{"backup-verify", "--source", "missing.db", "extra"},
	} {
		var stdout, stderr bytes.Buffer
		if status := runBackupCommand(args, &stdout, &stderr); status != 2 {
			t.Fatalf("args %v status %d, want usage error: %s", args, status, stderr.String())
		}
	}
	for _, command := range []string{"backup", "backup-verify"} {
		var stdout, stderr bytes.Buffer
		if status := runBackupCommand([]string{command, "--help"}, &stdout, &stderr); status != 0 {
			t.Fatalf("help for %s failed: %d: %s", command, status, stderr.String())
		}
	}
	if _, err := os.Stat(os.Getenv("ALMANAUT_DATA_DIR")); !os.IsNotExist(err) {
		t.Fatalf("CLI initialized server data directory: %v", err)
	}
}
