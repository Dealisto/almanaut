package store

import (
	"database/sql"
	"fmt"
	"strings"

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

// TagCount is a distinct tag name and the number of entities carrying it.
type TagCount struct {
	Name  string
	Count int
}

// Counts returns every distinct tag name with the number of entities that
// carry it, ordered by name.
func (r *TagRepo) Counts() ([]TagCount, error) {
	rows, err := r.db.Query(
		`SELECT name, COUNT(*) FROM tags GROUP BY name ORDER BY name`,
	)
	if err != nil {
		return nil, fmt.Errorf("query tag counts: %w", err)
	}
	defer rows.Close()
	counts := []TagCount{}
	for rows.Next() {
		var c TagCount
		if err := rows.Scan(&c.Name, &c.Count); err != nil {
			return nil, fmt.Errorf("scan tag count: %w", err)
		}
		counts = append(counts, c)
	}
	return counts, rows.Err()
}

// ListByName returns every tag row whose (normalized) name matches, ordered by
// entity_type then entity_id.
func (r *TagRepo) ListByName(name string) ([]domain.Tag, error) {
	rows, err := r.db.Query(
		`SELECT id, entity_type, entity_id, name FROM tags
		 WHERE name = ? ORDER BY entity_type, entity_id`,
		domain.NormalizeTag(name),
	)
	if err != nil {
		return nil, fmt.Errorf("query tags by name: %w", err)
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

// Search returns tag rows whose name contains q (case-insensitive substring),
// ordered by entity_type then entity_id. Tag names are stored lowercase, so the
// query is lowercased and matched with instr (no LIKE wildcards to escape).
func (r *TagRepo) Search(q string) ([]domain.Tag, error) {
	rows, err := r.db.Query(
		`SELECT id, entity_type, entity_id, name FROM tags
		 WHERE instr(name, ?) > 0 ORDER BY entity_type, entity_id`,
		strings.ToLower(strings.TrimSpace(q)),
	)
	if err != nil {
		return nil, fmt.Errorf("search tags: %w", err)
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
