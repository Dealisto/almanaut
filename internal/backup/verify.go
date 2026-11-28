// Package backup creates and verifies database backups without starting an
// Almanaut server. External configuration and secret files are not included.
package backup

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
)

// Verification describes a structurally valid Almanaut database. It is not a
// guarantee that an arbitrary server version can run that database.
type Verification struct {
	SchemaVersion string `json:"schema_version"`
	Migrations    int    `json:"migrations"`
	Bytes         int64  `json:"bytes"`
}

// Verify checks an existing database read-only. It does not migrate, bootstrap,
// initialize a database, or run integrations. Old Almanaut schemas are accepted.
func Verify(ctx context.Context, path string) (Verification, error) {
	db, err := openReadOnly(ctx, path)
	if err != nil {
		return Verification{}, err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Verification{}, fmt.Errorf("read database: %w", err)
	}
	defer tx.Rollback()
	info, err := schemaInfo(ctx, tx)
	if err != nil {
		return Verification{}, err
	}
	rows, err := tx.QueryContext(ctx, `PRAGMA integrity_check`)
	if err != nil {
		return Verification{}, fmt.Errorf("check database integrity: %w", err)
	}
	var results int
	for rows.Next() {
		var result string
		if err := rows.Scan(&result); err != nil {
			rows.Close()
			return Verification{}, fmt.Errorf("read integrity check: %w", err)
		}
		if result != "ok" {
			rows.Close()
			return Verification{}, fmt.Errorf("database integrity check failed: %s", result)
		}
		results++
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return Verification{}, fmt.Errorf("check database integrity: %w", err)
	}
	if results != 1 {
		return Verification{}, fmt.Errorf("database integrity check returned no definitive result")
	}
	rows, err = tx.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return Verification{}, fmt.Errorf("check database references: %w", err)
	}
	if rows.Next() {
		rows.Close()
		return Verification{}, fmt.Errorf("database has foreign key violations")
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return Verification{}, fmt.Errorf("check database references: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Verification{}, fmt.Errorf("finish verification: %w", err)
	}
	stat, err := os.Stat(path)
	if err != nil {
		return Verification{}, fmt.Errorf("stat database: %w", err)
	}
	info.Bytes = stat.Size()
	return info, ctx.Err()
}

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func schemaInfo(ctx context.Context, db queryer) (Verification, error) {
	var tables int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_schema WHERE type='table' AND name IN ('hosts', 'schema_migrations')`).Scan(&tables); err != nil {
		return Verification{}, fmt.Errorf("read Almanaut schema: %w", err)
	}
	if tables != 2 {
		return Verification{}, fmt.Errorf("not an Almanaut database: required tables are missing")
	}
	// These columns have existed since the initial schema. Do not require
	// current tables or run migrations just to recognize an older backup.
	var hosts int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM (SELECT id, name, type, ips FROM hosts LIMIT 0)`).Scan(&hosts); err != nil {
		return Verification{}, fmt.Errorf("not an Almanaut database: invalid hosts schema: %w", err)
	}
	var first int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version='0001_init.sql' AND applied_at IS NOT NULL`).Scan(&first); err != nil {
		return Verification{}, fmt.Errorf("not an Almanaut database: invalid migration history: %w", err)
	}
	if first != 1 {
		return Verification{}, fmt.Errorf("not an Almanaut database: initial migration is missing or duplicated")
	}
	var info Verification
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*), MAX(version) FROM schema_migrations`).Scan(&info.Migrations, &info.SchemaVersion); err != nil {
		return Verification{}, fmt.Errorf("read migration history: %w", err)
	}
	return info, nil
}

func openReadOnly(ctx context.Context, path string) (*sql.DB, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if path == "" {
		return nil, fmt.Errorf("an explicit source database path is required")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open source database: %w", err)
	}
	stat, err := f.Stat()
	if err != nil || !stat.Mode().IsRegular() {
		f.Close()
		return nil, fmt.Errorf("source database must be an existing regular file")
	}
	var header [16]byte
	_, err = io.ReadFull(f, header[:])
	f.Close()
	if err != nil || string(header[:]) != "SQLite format 3\x00" {
		return nil, fmt.Errorf("source is not a SQLite database")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve source database: %w", err)
	}
	filename := filepath.ToSlash(absolute)
	if filepath.VolumeName(absolute) != "" && filename[0] != '/' {
		filename = "/" + filename
	}
	u := url.URL{Scheme: "file", Path: filename}
	q := url.Values{"mode": {"ro"}, "_pragma": {"busy_timeout(5000)"}}
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, fmt.Errorf("open source database read-only: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("read source database: %w", err)
	}
	return db, nil
}
