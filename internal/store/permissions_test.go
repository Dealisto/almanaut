package store

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestNewDatabaseAndJournalsArePrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix file permissions are not available on Windows")
	}
	path := filepath.Join(t.TempDir(), "private.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE secrets (value TEXT); INSERT INTO secrets VALUES ('private')`); err != nil {
		t.Fatal(err)
	}
	assertMode := func(path string) {
		t.Helper()
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("%s permissions=%04o, want 0600", filepath.Base(path), got)
		}
	}
	assertMode(path)
	assertMode(path + "-wal")
	assertMode(path + "-shm")
	// A future migration or recovery can switch back to rollback journaling.
	// Its temporary journal must inherit the same private permissions.
	if _, err := db.Exec(`PRAGMA journal_mode=DELETE`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE secrets SET value='changed'`); err != nil {
		t.Fatal(err)
	}
	assertMode(path + "-journal")
}

func TestOpenPreservesExistingDatabasePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix file permissions are not available on Windows")
	}
	path := filepath.Join(t.TempDir(), "shared.db")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("existing database permissions were changed: info=%v err=%v", info, err)
	}
}

func TestOpenSpecialDatabasePathsDoNotCreatePersistentFiles(t *testing.T) {
	for _, path := range []string{"", ":memory:"} {
		t.Run(path, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			db, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if _, err := db.Exec(`CREATE TABLE example (id INTEGER)`); err != nil {
				t.Fatal(err)
			}
			files, err := os.ReadDir(dir)
			if err != nil || len(files) != 0 {
				t.Fatalf("special database path created persistent files: %v err=%v", files, err)
			}
		})
	}
}

func TestBackupDatabaseIsPrivateAndReadable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix file permissions are not available on Windows")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "private.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE secrets (value TEXT); INSERT INTO secrets VALUES ('retained')`); err != nil {
		t.Fatal(err)
	}
	if err := backupDatabase(db, path); err != nil {
		t.Fatal(err)
	}
	backups, err := filepath.Glob(path + ".backup-*")
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups=%v err=%v, want one backup", backups, err)
	}
	info, err := os.Stat(backups[0])
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("backup permissions: info=%v err=%v, want 0600", info, err)
	}
	copy, err := Open(backups[0])
	if err != nil {
		t.Fatal(err)
	}
	defer copy.Close()
	var value string
	if err := copy.QueryRow(`SELECT value FROM secrets`).Scan(&value); err != nil || value != "retained" {
		t.Fatalf("backup is not readable: value=%q err=%v", value, err)
	}
}

func TestBackupDatabaseFailureRemovesIncompleteFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	existing := path + ".backup-existing"
	if err := os.WriteFile(existing, []byte("previous backup"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := backupDatabase(db, path); err == nil {
		t.Fatal("backup of a closed database succeeded")
	}
	backups, err := filepath.Glob(path + ".backup-*")
	if err != nil || len(backups) != 1 || backups[0] != existing {
		t.Fatalf("failed backup left an incomplete file or removed an existing backup: %v err=%v", backups, err)
	}
	content, err := os.ReadFile(existing)
	if err != nil || string(content) != "previous backup" {
		t.Fatalf("existing backup changed: %q err=%v", content, err)
	}
}
