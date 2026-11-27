package store

import (
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate applies any embedded migrations not yet recorded in
// schema_migrations. Before applying pending migrations it writes a
// clean backup of the database next to dbPath. Safe to call repeatedly.
func Migrate(db *sql.DB, dbPath string) error {
	if _, err := db.Exec(
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TEXT NOT NULL
		)`,
	); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	applied, err := appliedVersions(db)
	if err != nil {
		return err
	}

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	var pending []string
	for _, f := range files {
		if !applied[f] {
			pending = append(pending, f)
		}
	}
	if len(pending) == 0 {
		return nil
	}

	if dbPath != "" {
		if err := backupDatabase(db, dbPath); err != nil {
			return fmt.Errorf("pre-migration backup: %w", err)
		}
	}

	for _, f := range pending {
		sqlBytes, err := migrationsFS.ReadFile("migrations/" + f)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", f, err)
		}
		if err := applyMigration(db, f, string(sqlBytes)); err != nil {
			return err
		}
	}
	return nil
}

// applyMigration rechecks the version after taking the write lock: another
// process may have applied it since Migrate built its list of pending files.
func applyMigration(db *sql.DB, version, statements string) error {
	return WithTx(db, func(tx *sql.Tx) error {
		var applied bool
		if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = ?)`, version).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %s: %w", version, err)
		}
		if applied {
			return nil
		}
		if _, err := tx.Exec(statements); err != nil {
			return fmt.Errorf("apply %s: %w", version, err)
		}
		if _, err := tx.Exec(
			`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
			version, time.Now().UTC().Format(time.RFC3339),
		); err != nil {
			return fmt.Errorf("record %s: %w", version, err)
		}
		return nil
	})
}

func appliedVersions(db *sql.DB) (map[string]bool, error) {
	rows, err := db.Query(`SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("query applied migrations: %w", err)
	}
	defer rows.Close()
	applied := map[string]bool{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("scan migration version: %w", err)
		}
		applied[v] = true
	}
	return applied, rows.Err()
}

// backupDatabase writes a consistent copy of the database using
// SQLite's VACUUM INTO, which is safe while the DB is open.
func backupDatabase(db *sql.DB, dbPath string) error {
	absolute, err := filepath.Abs(dbPath)
	if err != nil {
		return fmt.Errorf("resolve backup path: %w", err)
	}
	// VACUUM INTO interprets a leading "file:" as a URI. An absolute path
	// keeps literal filenames on the same destination reserved below, rather
	// than leaving an empty placeholder and creating another, public file.
	backup := fmt.Sprintf("%s.backup-%s", absolute, time.Now().UTC().Format("20060102-150405.000000000"))
	// VACUUM INTO accepts an empty existing file but otherwise creates it
	// with SQLite's public default mode, independent of the source's mode.
	// Reserve this name privately and never overwrite an existing backup.
	file, err := os.OpenFile(backup, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create backup %q: %w", backup, err)
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.Remove(backup)
		}
	}()
	if err := file.Close(); err != nil {
		return fmt.Errorf("close backup %q: %w", backup, err)
	}
	if _, err := db.Exec(`VACUUM INTO ?`, backup); err != nil {
		return fmt.Errorf("VACUUM INTO %q: %w", backup, err)
	}
	complete = true
	return nil
}
