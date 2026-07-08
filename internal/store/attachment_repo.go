package store

import (
	"database/sql"
	"fmt"

	"github.com/Dealisto/almanaut/internal/domain"
)

// AttachmentRepo persists file attachments (blob + metadata) in SQLite.
type AttachmentRepo struct {
	db DBTX
}

// NewAttachmentRepo returns an AttachmentRepo backed by db.
func NewAttachmentRepo(db *sql.DB) *AttachmentRepo { return &AttachmentRepo{db: db} }

// WithTx returns a copy of the repo whose operations run inside tx.
func (r *AttachmentRepo) WithTx(tx *sql.Tx) *AttachmentRepo { return &AttachmentRepo{db: tx} }

// Create inserts a (including its content blob) and returns the new id.
func (r *AttachmentRepo) Create(a domain.Attachment) (int64, error) {
	res, err := r.db.Exec(
		`INSERT INTO attachments (entity_type, entity_id, filename, content_type, size, content, uploaded_at, uploaded_by)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		a.EntityType, a.EntityID, a.Filename, a.ContentType, a.Size, a.Content, a.UploadedAt, a.UploadedBy,
	)
	if err != nil {
		return 0, fmt.Errorf("insert attachment: %w", err)
	}
	return res.LastInsertId()
}

// Get returns the full attachment (with content) for id, or ErrNotFound.
func (r *AttachmentRepo) Get(id int64) (domain.Attachment, error) {
	row := r.db.QueryRow(
		`SELECT id, entity_type, entity_id, filename, content_type, size, content, uploaded_at, uploaded_by
		 FROM attachments WHERE id = ?`, id,
	)
	var a domain.Attachment
	if err := row.Scan(&a.ID, &a.EntityType, &a.EntityID, &a.Filename, &a.ContentType, &a.Size, &a.Content, &a.UploadedAt, &a.UploadedBy); err != nil {
		return domain.Attachment{}, notFound(fmt.Errorf("scan attachment: %w", err))
	}
	return a, nil
}

// ListForEntity returns attachment METADATA (no content blob) for
// (entityType, entityID), oldest first. The content column is deliberately not
// selected so rendering a detail page never pulls file bytes into memory.
func (r *AttachmentRepo) ListForEntity(entityType string, entityID int64) ([]domain.Attachment, error) {
	rows, err := r.db.Query(
		`SELECT id, entity_type, entity_id, filename, content_type, size, uploaded_at, uploaded_by
		 FROM attachments WHERE entity_type = ? AND entity_id = ? ORDER BY uploaded_at, id`,
		entityType, entityID,
	)
	if err != nil {
		return nil, fmt.Errorf("query attachments: %w", err)
	}
	defer rows.Close()
	out := []domain.Attachment{}
	for rows.Next() {
		var a domain.Attachment
		if err := rows.Scan(&a.ID, &a.EntityType, &a.EntityID, &a.Filename, &a.ContentType, &a.Size, &a.UploadedAt, &a.UploadedBy); err != nil {
			return nil, fmt.Errorf("scan attachment: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Delete removes the attachment with id; ErrNotFound if none matched.
func (r *AttachmentRepo) Delete(id int64) error {
	res, err := r.db.Exec(`DELETE FROM attachments WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete attachment: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}

// DeleteByEntity removes every attachment for (entityType, id). Used to clean up
// when an entity is deleted.
func (r *AttachmentRepo) DeleteByEntity(entityType string, id int64) error {
	if _, err := r.db.Exec(
		`DELETE FROM attachments WHERE entity_type = ? AND entity_id = ?`,
		entityType, id,
	); err != nil {
		return fmt.Errorf("delete attachments for entity: %w", err)
	}
	return nil
}
