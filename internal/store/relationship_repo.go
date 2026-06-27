package store

import (
	"database/sql"
	"fmt"

	"github.com/Dealisto/almanaut/internal/domain"
)

// RelationshipRepo persists Relationship edges in SQLite.
type RelationshipRepo struct {
	db *sql.DB
}

// NewRelationshipRepo returns a RelationshipRepo backed by db.
func NewRelationshipRepo(db *sql.DB) *RelationshipRepo {
	return &RelationshipRepo{db: db}
}

// Create inserts rel and returns its new ID.
func (r *RelationshipRepo) Create(rel domain.Relationship) (int64, error) {
	res, err := r.db.Exec(
		`INSERT INTO relationships (from_type, from_id, to_type, to_id, kind)
		 VALUES (?, ?, ?, ?, ?)`,
		rel.FromType, rel.FromID, rel.ToType, rel.ToID, rel.Kind,
	)
	if err != nil {
		return 0, fmt.Errorf("insert relationship: %w", err)
	}
	return res.LastInsertId()
}

// List returns all relationships ordered by id.
func (r *RelationshipRepo) List() ([]domain.Relationship, error) {
	return r.query(
		`SELECT id, from_type, from_id, to_type, to_id, kind FROM relationships ORDER BY id`,
	)
}

// ListByTo returns all relationships whose "to" endpoint is (toType, toID).
func (r *RelationshipRepo) ListByTo(toType string, toID int64) ([]domain.Relationship, error) {
	return r.query(
		`SELECT id, from_type, from_id, to_type, to_id, kind
		 FROM relationships WHERE to_type = ? AND to_id = ?`,
		toType, toID,
	)
}

// ListForEntity returns all relationships where (entityType, entityID) is either
// the from or the to endpoint, ordered by id.
func (r *RelationshipRepo) ListForEntity(entityType string, entityID int64) ([]domain.Relationship, error) {
	return r.query(
		`SELECT id, from_type, from_id, to_type, to_id, kind
		 FROM relationships
		 WHERE (from_type = ? AND from_id = ?) OR (to_type = ? AND to_id = ?)
		 ORDER BY id`,
		entityType, entityID, entityType, entityID,
	)
}

// Delete removes the relationship with the given id.
func (r *RelationshipRepo) Delete(id int64) error {
	if _, err := r.db.Exec(`DELETE FROM relationships WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete relationship: %w", err)
	}
	return nil
}

// DeleteByEntity removes every relationship where (entityType, id) is the from
// or the to endpoint. Used to clean up when an entity is deleted.
func (r *RelationshipRepo) DeleteByEntity(entityType string, id int64) error {
	_, err := r.db.Exec(
		`DELETE FROM relationships
		 WHERE (from_type = ? AND from_id = ?) OR (to_type = ? AND to_id = ?)`,
		entityType, id, entityType, id,
	)
	if err != nil {
		return fmt.Errorf("delete relationships for entity: %w", err)
	}
	return nil
}

func (r *RelationshipRepo) query(sqlStr string, args ...any) ([]domain.Relationship, error) {
	rows, err := r.db.Query(sqlStr, args...)
	if err != nil {
		return nil, fmt.Errorf("query relationships: %w", err)
	}
	defer rows.Close()
	rels := []domain.Relationship{}
	for rows.Next() {
		rel, err := scanRelationship(rows)
		if err != nil {
			return nil, err
		}
		rels = append(rels, rel)
	}
	return rels, rows.Err()
}

func scanRelationship(s scanner) (domain.Relationship, error) {
	var rel domain.Relationship
	if err := s.Scan(&rel.ID, &rel.FromType, &rel.FromID, &rel.ToType, &rel.ToID, &rel.Kind); err != nil {
		return domain.Relationship{}, fmt.Errorf("scan relationship: %w", err)
	}
	return rel, nil
}
