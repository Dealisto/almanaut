package store

import (
	"database/sql"
	"fmt"

	"github.com/Dealisto/almanaut/internal/domain"
)

// TagRepo persists Tag rows in SQLite.
type TagRepo struct {
	db *sql.DB
}

// NewTagRepo returns a TagRepo backed by db.
func NewTagRepo(db *sql.DB) *TagRepo {
	return &TagRepo{db: db}
}

// Add stores a tag (normalizing its name). Adding the same tag twice is a no-op
// thanks to the UNIQUE index.
func (r *TagRepo) Add(t domain.Tag) error {
	name := domain.NormalizeTag(t.Name)
	_, err := r.db.Exec(
		`INSERT OR IGNORE INTO tags (entity_type, entity_id, name) VALUES (?, ?, ?)`,
		t.EntityType, t.EntityID, name,
	)
	if err != nil {
		return fmt.Errorf("insert tag: %w", err)
	}
	return nil
}

// Delete removes the tag with the given id.
func (r *TagRepo) Delete(id int64) error {
	if _, err := r.db.Exec(`DELETE FROM tags WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete tag: %w", err)
	}
	return nil
}

// ListForEntity returns the tags attached to (entityType, entityID), ordered by name.
func (r *TagRepo) ListForEntity(entityType string, entityID int64) ([]domain.Tag, error) {
	rows, err := r.db.Query(
		`SELECT id, entity_type, entity_id, name FROM tags
		 WHERE entity_type = ? AND entity_id = ? ORDER BY name`,
		entityType, entityID,
	)
	if err != nil {
		return nil, fmt.Errorf("query tags: %w", err)
	}
	defer rows.Close()
	tags := []domain.Tag{}
	for rows.Next() {
		var t domain.Tag
		if err := rows.Scan(&t.ID, &t.EntityType, &t.EntityID, &t.Name); err != nil {
			return nil, fmt.Errorf("scan tag: %w", err)
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}
