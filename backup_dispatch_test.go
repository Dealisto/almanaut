package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBackupCommandsBypassServerStartup(t *testing.T) {
	if os.Getenv("ALMANAUT_TEST_BACKUP_DISPATCH_PROCESS") == "1" {
		for i, arg := range os.Args {
			if arg == "--" {
				os.Args = append([]string{"almanaut"}, os.Args[i+1:]...)
				main()
				return
			}
		}
		t.Fatal("missing subprocess command")
	}
	for _, command := range []string{"backup", "backup-verify"} {
		t.Run(command, func(t *testing.T) {
			dir := t.TempDir()
			dataDir := filepath.Join(dir, "must-not-be-created")
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestBackupCommandsBypassServerStartup$", "--", command, "--help")
			for _, entry := range os.Environ() {
				if !strings.HasPrefix(entry, "ALMANAUT_") {
					cmd.Env = append(cmd.Env, entry)
				}
			}
			cmd.Env = append(cmd.Env,
				"ALMANAUT_TEST_BACKUP_DISPATCH_PROCESS=1",
				"ALMANAUT_DATA_DIR="+dataDir,
				"ALMANAUT_AUTH_PASS_FILE="+filepath.Join(dir, "missing-secret"),
				"ALMANAUT_DISCOVERY_SOURCES_FILE="+filepath.Join(dir, "missing-discovery-config"),
			)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%s loaded startup configuration or failed: %v\n%s", command, err, output)
			}
			if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
				t.Fatalf("%s touched server data directory: %v", command, err)
			}
		})
	}
}
