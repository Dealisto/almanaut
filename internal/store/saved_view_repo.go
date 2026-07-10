package store

import (
	"database/sql"
	"fmt"

	"github.com/Dealisto/almanaut/internal/domain"
)

// SavedViewRepo persists per-user saved list views in SQLite.
type SavedViewRepo struct {
	db DBTX
}

// NewSavedViewRepo returns a SavedViewRepo backed by db.
func NewSavedViewRepo(db *sql.DB) *SavedViewRepo { return &SavedViewRepo{db: db} }

// WithTx returns a copy of the repo whose operations run inside tx.
func (r *SavedViewRepo) WithTx(tx *sql.Tx) *SavedViewRepo { return &SavedViewRepo{db: tx} }

// Create inserts v and returns its new ID.
func (r *SavedViewRepo) Create(v domain.SavedView) (int64, error) {
	res, err := r.db.Exec(
		`INSERT INTO saved_views (user_id, entity_type, name, query, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		v.UserID, v.EntityType, v.Name, v.Query, v.CreatedAt,
	)
	if err != nil {
		return 0, fmt.Errorf("insert saved view: %w", err)
	}
	return res.LastInsertId()
}

// ListForUser returns userID's saved views, ordered by entity type then name so
// the sidebar can group them without re-sorting.
func (r *SavedViewRepo) ListForUser(userID int64) ([]domain.SavedView, error) {
	rows, err := r.db.Query(
		`SELECT id, user_id, entity_type, name, query, created_at
		 FROM saved_views WHERE user_id = ? ORDER BY entity_type, name`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("query saved views: %w", err)
	}
	defer rows.Close()
	views := []domain.SavedView{}
	for rows.Next() {
		var v domain.SavedView
		if err := rows.Scan(&v.ID, &v.UserID, &v.EntityType, &v.Name, &v.Query, &v.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan saved view: %w", err)
		}
		views = append(views, v)
	}
	return views, rows.Err()
}

// Rename changes the name of view id, but only when it belongs to userID, so a
// user cannot touch another user's view. A no-op reports ErrNotFound.
func (r *SavedViewRepo) Rename(id, userID int64, name string) error {
	res, err := r.db.Exec(
		`UPDATE saved_views SET name = ? WHERE id = ? AND user_id = ?`,
		name, id, userID,
	)
	if err != nil {
		return fmt.Errorf("rename saved view: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}

// Delete removes view id, but only when it belongs to userID. A no-op reports
// ErrNotFound.
func (r *SavedViewRepo) Delete(id, userID int64) error {
	res, err := r.db.Exec(`DELETE FROM saved_views WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return fmt.Errorf("delete saved view: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}
