package store

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestBackupPreservesLiteralRelativeDatabasePath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows filenames cannot contain a colon")
	}
	t.Chdir(t.TempDir())
	// This is a literal filename, not a SQLite URI; Open supports it.
	path := "file:inventory.db"
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE backup_marker (value TEXT); INSERT INTO backup_marker VALUES ('restorable')`); err != nil {
		t.Fatal(err)
	}
	if err := backupDatabase(db, path); err != nil {
		t.Fatal(err)
	}
	backups, err := filepath.Glob(path + ".backup-*")
	if err != nil || len(backups) != 1 {
		t.Fatalf("backup destinations: %v, %v", backups, err)
	}
	restored, err := Open(backups[0])
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	var value string
	if err := restored.QueryRow(`SELECT value FROM backup_marker`).Scan(&value); err != nil || value != "restorable" {
		t.Fatalf("reserved backup is not restorable: value=%q error=%v", value, err)
	}
	unexpected, err := filepath.Glob("inventory.db.backup-*")
	if err != nil || len(unexpected) != 0 {
		t.Fatalf("backup escaped its literal destination: %v, %v", unexpected, err)
	}
}
