package store

import (
	"database/sql"
	"embed"
	"fmt"
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
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("begin tx for %s: %w", f, err)
		}
		if _, err := tx.Exec(string(sqlBytes)); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply %s: %w", f, err)
		}
		if _, err := tx.Exec(
			`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
			f, time.Now().UTC().Format(time.RFC3339),
		); err != nil {
			tx.Rollback()
			return fmt.Errorf("record %s: %w", f, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit %s: %w", f, err)
		}
	}
	return nil
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
			return nil, err
		}
		applied[v] = true
	}
	return applied, rows.Err()
}

// backupDatabase writes a consistent copy of the database using
// SQLite's VACUUM INTO, which is safe while the DB is open.
func backupDatabase(db *sql.DB, dbPath string) error {
	backup := fmt.Sprintf("%s.backup-%s", dbPath, time.Now().UTC().Format("20060102-150405"))
	_, err := db.Exec(`VACUUM INTO ?`, backup)
	return err
}
