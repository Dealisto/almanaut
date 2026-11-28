package backup

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
	"golang.org/x/crypto/bcrypt"
)

func oldDatabase(t *testing.T) (*sql.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "source.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	_, err = db.Exec(`
		CREATE TABLE schema_migrations (version TEXT PRIMARY KEY, applied_at TEXT NOT NULL);
		INSERT INTO schema_migrations VALUES ('0001_init.sql', '2020-01-01T00:00:00Z');
		CREATE TABLE hosts (id INTEGER PRIMARY KEY, name TEXT, type TEXT, ips TEXT);
		INSERT INTO hosts VALUES (1, 'legacy-host', 'physical', '["10.0.0.1"]');`)
	if err != nil {
		t.Fatal(err)
	}
	return db, path
}

func TestCreateIncludesCommittedWALAndConcurrentTransactionConsistency(t *testing.T) {
	db, source := oldDatabase(t)
	if _, err := db.Exec(`CREATE TABLE checkpoint (id INTEGER PRIMARY KEY, value INTEGER);
		INSERT INTO checkpoint VALUES (1, 0), (2, 0);
		CREATE TABLE attachments (id INTEGER PRIMARY KEY, content BLOB);
		INSERT INTO attachments VALUES (1, X'000102F0FF');`); err != nil {
		t.Fatal(err)
	}
	if stat, err := os.Stat(source + "-wal"); err != nil || stat.Size() == 0 {
		t.Fatalf("expected live committed WAL: %v, %v", stat, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	writerErrors := make(chan error, 1)
	go func() {
		defer wg.Done()
		close(started)
		for ctx.Err() == nil {
			err := store.WithTxContext(ctx, db, func(tx *sql.Tx) error {
				_, err := tx.ExecContext(ctx, `UPDATE checkpoint SET value=value+1 WHERE id=1`)
				if err != nil {
					return err
				}
				_, err = tx.ExecContext(ctx, `UPDATE checkpoint SET value=value+1 WHERE id=2`)
				return err
			})
			if err != nil && ctx.Err() == nil {
				writerErrors <- err
				return
			}
		}
	}()
	<-started
	destination := filepath.Join(t.TempDir(), "snapshot.db")
	info, err := Create(context.Background(), source, destination)
	cancel()
	wg.Wait()
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-writerErrors:
		t.Fatal(err)
	default:
	}
	if info.Migrations != 1 || info.SchemaVersion != "0001_init.sql" {
		t.Fatalf("older schema was not retained: %+v", info)
	}
	restored, err := openReadOnly(context.Background(), destination)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	var first, second int
	if err := restored.QueryRow(`SELECT a.value, b.value FROM checkpoint a, checkpoint b WHERE a.id=1 AND b.id=2`).Scan(&first, &second); err != nil || first != second {
		t.Fatalf("torn transaction in backup: %d, %d: %v", first, second, err)
	}
	var content string
	if err := restored.QueryRow(`SELECT hex(content) FROM attachments WHERE id=1`).Scan(&content); err != nil || content != "000102F0FF" {
		t.Fatalf("committed attachment missing from WAL snapshot: %q: %v", content, err)
	}
	if runtime.GOOS != "windows" {
		stat, err := os.Stat(destination)
		if err != nil || stat.Mode().Perm() != 0o600 {
			t.Fatalf("snapshot is not private: %v: %v", stat, err)
		}
	}
}

func TestCreateAndVerifyNeverCreateMissingSource(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "missing.db")
	destination := filepath.Join(dir, "copy.db")
	if _, err := Verify(context.Background(), source); err == nil {
		t.Fatal("verification created a missing database")
	}
	if _, err := Create(context.Background(), source, destination); err == nil {
		t.Fatal("backup created a missing database")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("missing source produced files: %v: %v", entries, err)
	}
}

func TestVerifyRejectsWrongCorruptAndEmptyDatabases(t *testing.T) {
	for _, name := range []string{"empty", "wrong-schema", "corrupt"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "bad.db")
			switch name {
			case "wrong-schema":
				db, err := store.Open(path)
				if err != nil {
					t.Fatal(err)
				}
				_, err = db.Exec(`CREATE TABLE unrelated (id INTEGER)`)
				db.Close()
				if err != nil {
					t.Fatal(err)
				}
			case "corrupt":
				if err := os.WriteFile(path, []byte("SQLite format 3\x00"+strings.Repeat("broken", 1024)), 0o600); err != nil {
					t.Fatal(err)
				}
			default:
				if err := os.WriteFile(path, nil, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			before, _ := os.ReadFile(path)
			if _, err := Verify(context.Background(), path); err == nil {
				t.Fatal("invalid database verified successfully")
			}
			after, _ := os.ReadFile(path)
			if string(before) != string(after) {
				t.Fatal("verification modified its input")
			}
			destination := filepath.Join(t.TempDir(), "backup.db")
			if _, err := Create(context.Background(), path, destination); err == nil {
				t.Fatal("invalid database backed up successfully")
			}
			if _, err := os.Stat(destination); !os.IsNotExist(err) {
				t.Fatalf("invalid backup left a destination: %v", err)
			}
		})
	}
}

func TestVerifyOldBackupIsReadOnlyAndDoesNotMigrate(t *testing.T) {
	db, source := oldDatabase(t)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(source)
	if err := os.Chmod(source, 0o400); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(source, 0o600)
	info, err := Verify(context.Background(), source)
	if err != nil || info.Migrations != 1 {
		t.Fatalf("old database verification: %+v: %v", info, err)
	}
	after, _ := os.ReadFile(source)
	if string(before) != string(after) {
		t.Fatal("verification changed database bytes")
	}
}

func TestCreateDoesNotOverwriteAndCancellationCleansUp(t *testing.T) {
	_, source := oldDatabase(t)
	destination := filepath.Join(t.TempDir(), "existing.db")
	if err := os.WriteFile(destination, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Create(context.Background(), source, destination); err == nil {
		t.Fatal("existing destination overwritten")
	}
	content, _ := os.ReadFile(destination)
	if string(content) != "keep me" {
		t.Fatal("existing destination changed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	other := filepath.Join(t.TempDir(), "canceled.db")
	if _, err := Create(ctx, source, other); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation not propagated: %v", err)
	}
	if _, err := os.Stat(other); !os.IsNotExist(err) {
		t.Fatalf("canceled backup produced a file: %v", err)
	}
}

func TestManagerRetainsOwnedVerifiedSnapshotsAndReadsAfterRestart(t *testing.T) {
	db, source := oldDatabase(t)
	directory := t.TempDir()
	m, err := New(db, source, directory, 2, "test-version")
	if err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(directory, "almanaut-backup-123")
	if err := os.Mkdir(foreign, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(foreign, "notes.txt"), []byte("unrelated"), 0o600); err != nil {
		t.Fatal(err)
	}
	for range 4 {
		if err := m.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	restarted, err := New(db, source, directory, 2, "other-version")
	if err != nil {
		t.Fatal(err)
	}
	list, err := restarted.List(context.Background())
	if err != nil || len(list) != 2 {
		t.Fatalf("retained snapshots: %+v: %v", list, err)
	}
	for _, snapshot := range list {
		if snapshot.ServerVersion != "test-version" {
			t.Fatal("manifest lost version information")
		}
		path := filepath.Join(directory, snapshot.Directory, databaseName)
		if _, err := Verify(context.Background(), path); err != nil {
			t.Fatal(err)
		}
		digest, err := hashFile(context.Background(), path)
		if err != nil || digest != snapshot.SHA256 {
			t.Fatalf("incorrect manifest checksum: %v", err)
		}
		if runtime.GOOS != "windows" {
			for name, want := range map[string]os.FileMode{snapshot.Directory: 0o700, filepath.Join(snapshot.Directory, manifestName): 0o600} {
				stat, err := os.Stat(filepath.Join(directory, name))
				if err != nil || stat.Mode().Perm() != want {
					t.Fatalf("permissions for %s: %v: %v", name, stat, err)
				}
			}
		}
	}
	content, err := os.ReadFile(filepath.Join(foreign, "notes.txt"))
	if err != nil || string(content) != "unrelated" {
		t.Fatalf("retention touched foreign data: %q: %v", content, err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 3 {
		t.Fatalf("unexpected artifacts: %v: %v", entries, err)
	}
}

func TestManagerFailureDoesNotRunRetention(t *testing.T) {
	db, source := oldDatabase(t)
	directory := t.TempDir()
	m, err := New(db, source, directory, 3, "test")
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := m.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	m.keep = 1
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := m.Run(context.Background()); err == nil {
		t.Fatal("backup of closed database succeeded")
	}
	list, err := m.List(context.Background())
	if err != nil || len(list) != 2 {
		t.Fatalf("failed backup pruned successful snapshots: %+v: %v", list, err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 2 {
		t.Fatalf("failed backup left a directory: %v: %v", entries, err)
	}
}

func TestManagerPublicationFailureNeverPrunesEarlierBackups(t *testing.T) {
	db, source := oldDatabase(t)
	directory := t.TempDir()
	m, err := New(db, source, directory, 1, "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	before, err := m.List(context.Background())
	if err != nil || len(before) != 1 {
		t.Fatalf("initial snapshot: %+v: %v", before, err)
	}
	want := errors.New("directory sync failed")
	var synced []string
	m.syncDirectory = func(path string) error {
		synced = append(synced, path)
		if path == directory {
			return want
		}
		return store.SyncDirectory(path)
	}
	if err := m.Run(context.Background()); !errors.Is(err, want) {
		t.Fatalf("publication error was not propagated: %v", err)
	}
	if len(synced) != 2 || synced[1] != directory {
		t.Fatalf("run directory and root were not synced in order: %v", synced)
	}
	after, err := m.List(context.Background())
	if err != nil || len(after) != 1 || after[0].Directory != before[0].Directory {
		t.Fatalf("publication failure pruned older snapshot: %+v: %v", after, err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 1 {
		t.Fatalf("failed publication left an artifact: %v: %v", entries, err)
	}
}

func TestManagerNeverPrunesNewSnapshotAfterClockReversal(t *testing.T) {
	db, source := oldDatabase(t)
	directory := t.TempDir()
	m, err := New(db, source, directory, 1, "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	list, err := m.List(context.Background())
	if err != nil || len(list) != 1 {
		t.Fatalf("initial snapshot: %+v: %v", list, err)
	}
	old := list[0]
	old.CreatedAt = time.Now().Add(24 * time.Hour)
	data, _ := json.Marshal(old)
	if err := os.WriteFile(filepath.Join(directory, old.Directory, manifestName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	list, err = m.List(context.Background())
	if err != nil || len(list) != 1 || list[0].Directory == old.Directory {
		t.Fatalf("current snapshot was not retained: %+v: %v", list, err)
	}
}

func TestManagerRejectsMismatchedSourceHandle(t *testing.T) {
	db, _ := oldDatabase(t)
	_, other := oldDatabase(t)
	if _, err := New(db, other, t.TempDir(), 7, "test"); err == nil {
		t.Fatal("accepted source path for another database")
	}
}

func TestRestoreCurrentSchemaPreservesCredentialsAndAttachmentContent(t *testing.T) {
	source := filepath.Join(t.TempDir(), "almanaut.db")
	db, err := store.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := store.Migrate(db, ""); err != nil {
		t.Fatal(err)
	}
	password := "restore-test-password"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	userID, err := store.NewUserRepo(db).Create(domain.User{
		Username: "restore-admin", Role: domain.RoleAdmin, PasswordHash: string(hash),
		CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	hostID, err := store.NewHostRepo(db).Create(domain.Host{Name: "restore-host", Type: "physical"})
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("restorable attachment\x00\xff")
	attachmentID, err := store.NewAttachmentRepo(db).Create(domain.Attachment{
		EntityType: "host", EntityID: hostID, Filename: "manual.bin",
		ContentType: "application/octet-stream", Size: int64(len(content)), Content: content,
		UploadedAt: "2026-01-01T00:00:00Z", UploadedBy: "restore-admin",
	})
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "restored.db")
	if _, err := Create(context.Background(), source, destination); err != nil {
		t.Fatal(err)
	}
	// Reopen the restored database with the application's normal repositories,
	// without starting jobs or generating fresh accounts.
	restored, err := store.Open(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	user, err := store.NewUserRepo(restored).GetByUsername("restore-admin")
	if err != nil || user.ID != userID || user.Role != domain.RoleAdmin {
		t.Fatalf("restored administrator: %+v: %v", user, err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		t.Fatalf("restored credentials no longer authenticate: %v", err)
	}
	attachment, err := store.NewAttachmentRepo(restored).Get(attachmentID)
	if err != nil || attachment.EntityID != hostID || !bytes.Equal(attachment.Content, content) {
		t.Fatalf("restored attachment content or owner changed: %+v: %v", attachment, err)
	}
}

func TestManagerIgnoresSymlinksAndDirectoriesWithUnrelatedFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks may require additional privileges on Windows")
	}
	db, source := oldDatabase(t)
	directory := t.TempDir()
	m, err := New(db, source, directory, 1, "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	list, err := m.List(context.Background())
	if err != nil || len(list) != 1 {
		t.Fatalf("initial manifest: %+v: %v", list, err)
	}
	old := filepath.Join(directory, list[0].Directory)
	if err := os.WriteFile(filepath.Join(old, "keep.txt"), []byte("foreign"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(old, filepath.Join(directory, "almanaut-backup-123")); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := m.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Verify(context.Background(), filepath.Join(old, databaseName)); err != nil {
		t.Fatalf("retention deleted directory with unrelated content: %v", err)
	}
	list, err = m.List(context.Background())
	if err != nil || len(list) != 1 {
		t.Fatalf("foreign directory or symlink counted as owned: %+v: %v", list, err)
	}
}

func TestManagerListToleratesConcurrentRetention(t *testing.T) {
	db, source := oldDatabase(t)
	m, err := New(db, source, t.TempDir(), 1, "test")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	errors := make(chan error, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for ctx.Err() == nil {
			if _, err := m.List(context.Background()); err != nil {
				errors <- err
				return
			}
		}
	}()
	for range 20 {
		if err := m.Run(context.Background()); err != nil {
			cancel()
			wg.Wait()
			t.Fatal(err)
		}
	}
	cancel()
	wg.Wait()
	select {
	case err := <-errors:
		t.Fatalf("listing snapshots raced with retention: %v", err)
	default:
	}
}
