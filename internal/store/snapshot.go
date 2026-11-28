package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// SnapshotDatabase writes a consistent SQLite snapshot, including committed WAL
// data, to a new private file. It never overwrites a destination. The caller owns
// verification and retention; this also supports databases before migrations.
// Do not call it from a transaction or cap the database's connection pool.
func SnapshotDatabase(ctx context.Context, db *sql.DB, destination string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if destination == "" {
		return fmt.Errorf("a snapshot destination is required")
	}
	absolute, err := filepath.Abs(destination)
	if err != nil {
		return fmt.Errorf("resolve snapshot path: %w", err)
	}
	// An absolute filename prevents VACUUM INTO from interpreting a literal
	// leading "file:" as a URI. Reserving it prevents SQLite's default 0644.
	f, err := os.OpenFile(absolute, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create snapshot: %w", err)
	}
	complete := false
	defer func() {
		_ = f.Close()
		if !complete {
			_ = os.Remove(absolute)
		}
	}()
	if _, err := db.ExecContext(ctx, `VACUUM INTO ?`, absolute); err != nil {
		return fmt.Errorf("snapshot database: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync snapshot: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close snapshot: %w", err)
	}
	if err := SyncDirectory(filepath.Dir(absolute)); err != nil {
		return fmt.Errorf("sync snapshot directory: %w", err)
	}
	complete = true
	return nil
}

// SyncDirectory persists directory entries on platforms that support syncing
// directory handles. Windows does not support os.File.Sync on directories;
// individual files are still flushed there, and directory durability follows
// the filesystem's guarantees.
func SyncDirectory(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
