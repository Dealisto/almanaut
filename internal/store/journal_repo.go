package store

import (
	"database/sql"
	"fmt"

	"github.com/Dealisto/almanaut/internal/domain"
)

// JournalRepo persists manual journal entries.
type JournalRepo struct {
	db DBTX
}

// NewJournalRepo returns a JournalRepo backed by db.
func NewJournalRepo(db *sql.DB) *JournalRepo { return &JournalRepo{db: db} }

// WithTx returns a copy of the repo whose operations run inside tx.
func (r *JournalRepo) WithTx(tx *sql.Tx) *JournalRepo { return &JournalRepo{db: tx} }

// Create inserts e and returns its new id.
func (r *JournalRepo) Create(e domain.JournalEntry) (int64, error) {
	res, err := r.db.Exec(
		`INSERT INTO journal_entries (entity_type, entity_id, kind, body, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		e.EntityType, e.EntityID, e.Kind, e.Body, e.CreatedAt,
	)
	if err != nil {
		return 0, fmt.Errorf("insert journal entry: %w", err)
	}
	return res.LastInsertId()
}

// Get returns the entry with the given id, or ErrNotFound.
func (r *JournalRepo) Get(id int64) (domain.JournalEntry, error) {
	row := r.db.QueryRow(
		`SELECT id, entity_type, entity_id, kind, body, created_at
		 FROM journal_entries WHERE id = ?`, id,
	)
	return scanJournal(row)
}

// ListForEntity returns the entries for (entityType, entityID), newest first.
func (r *JournalRepo) ListForEntity(entityType string, entityID int64) ([]domain.JournalEntry, error) {
	return r.query(
		`SELECT id, entity_type, entity_id, kind, body, created_at
		 FROM journal_entries WHERE entity_type = ? AND entity_id = ? ORDER BY id DESC`,
		entityType, entityID,
	)
}

// List returns every entry ordered by id (used by export).
func (r *JournalRepo) List() ([]domain.JournalEntry, error) {
	return r.query(
		`SELECT id, entity_type, entity_id, kind, body, created_at
		 FROM journal_entries ORDER BY id`,
	)
}

// Delete removes the entry with the given id.
func (r *JournalRepo) Delete(id int64) error {
	if _, err := r.db.Exec(`DELETE FROM journal_entries WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete journal entry: %w", err)
	}
	return nil
}

// DeleteByEntity removes every entry attached to (entityType, id). Used to clean
// up when an entity is deleted.
func (r *JournalRepo) DeleteByEntity(entityType string, id int64) error {
	if _, err := r.db.Exec(
		`DELETE FROM journal_entries WHERE entity_type = ? AND entity_id = ?`,
		entityType, id,
	); err != nil {
		return fmt.Errorf("delete journal entries for entity: %w", err)
	}
	return nil
}

func (r *JournalRepo) query(sqlStr string, args ...any) ([]domain.JournalEntry, error) {
	rows, err := r.db.Query(sqlStr, args...)
	if err != nil {
		return nil, fmt.Errorf("query journal entries: %w", err)
	}
	defer rows.Close()
	entries := []domain.JournalEntry{}
	for rows.Next() {
		e, err := scanJournal(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func scanJournal(s scanner) (domain.JournalEntry, error) {
	var e domain.JournalEntry
	if err := s.Scan(&e.ID, &e.EntityType, &e.EntityID, &e.Kind, &e.Body, &e.CreatedAt); err != nil {
		return domain.JournalEntry{}, notFound(fmt.Errorf("scan journal entry: %w", err))
	}
	return e, nil
}
