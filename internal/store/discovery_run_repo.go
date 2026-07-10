package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// DiscoveryRun is one scheduled discovery run's history record.
type DiscoveryRun struct {
	ID         int64
	Source     string
	StartedAt  time.Time
	FinishedAt time.Time
	FoundCount int
	NewCount   int
	Error      string
	NewKeys    []string
}

// DiscoveryRunRepo persists scheduled discovery run history.
type DiscoveryRunRepo struct {
	db DBTX
}

func NewDiscoveryRunRepo(db *sql.DB) *DiscoveryRunRepo { return &DiscoveryRunRepo{db: db} }

func (r *DiscoveryRunRepo) WithTx(tx *sql.Tx) *DiscoveryRunRepo { return &DiscoveryRunRepo{db: tx} }

// Record inserts a run and returns its new id.
func (r *DiscoveryRunRepo) Record(run DiscoveryRun) (int64, error) {
	keys, err := json.Marshal(nonNil(run.NewKeys))
	if err != nil {
		return 0, fmt.Errorf("marshal new_keys: %w", err)
	}
	res, err := r.db.Exec(
		`INSERT INTO discovery_runs (source, started_at, finished_at, found_count, new_count, error, new_keys)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		run.Source, run.StartedAt.UTC().Format(time.RFC3339), run.FinishedAt.UTC().Format(time.RFC3339),
		run.FoundCount, run.NewCount, run.Error, string(keys),
	)
	if err != nil {
		return 0, fmt.Errorf("insert discovery run: %w", err)
	}
	return res.LastInsertId()
}

// Latest returns the most recent run for source, or store.ErrNotFound.
func (r *DiscoveryRunRepo) Latest(source string) (DiscoveryRun, error) {
	row := r.db.QueryRow(
		`SELECT id, source, started_at, finished_at, found_count, new_count, error, new_keys
		 FROM discovery_runs WHERE source = ? ORDER BY id DESC LIMIT 1`, source,
	)
	return scanDiscoveryRun(row)
}

// List returns up to limit runs, most recent first.
func (r *DiscoveryRunRepo) List(limit int) ([]DiscoveryRun, error) {
	rows, err := r.db.Query(
		`SELECT id, source, started_at, finished_at, found_count, new_count, error, new_keys
		 FROM discovery_runs ORDER BY id DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query discovery runs: %w", err)
	}
	defer rows.Close()
	out := []DiscoveryRun{}
	for rows.Next() {
		run, err := scanDiscoveryRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

func scanDiscoveryRun(s scanner) (DiscoveryRun, error) {
	var run DiscoveryRun
	var started, finished, keys string
	if err := s.Scan(&run.ID, &run.Source, &started, &finished, &run.FoundCount, &run.NewCount, &run.Error, &keys); err != nil {
		return DiscoveryRun{}, notFound(fmt.Errorf("scan discovery run: %w", err))
	}
	run.StartedAt, _ = time.Parse(time.RFC3339, started)
	run.FinishedAt, _ = time.Parse(time.RFC3339, finished)
	if err := json.Unmarshal([]byte(keys), &run.NewKeys); err != nil {
		return DiscoveryRun{}, fmt.Errorf("unmarshal new_keys: %w", err)
	}
	return run, nil
}
