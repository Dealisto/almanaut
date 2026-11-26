package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/Dealisto/almanaut/internal/domain"
)

// CustomFieldRepo persists custom field definitions and values in SQLite.
type CustomFieldRepo struct {
	db DBTX
}

// NewCustomFieldRepo returns a CustomFieldRepo backed by db.
func NewCustomFieldRepo(db *sql.DB) *CustomFieldRepo {
	return &CustomFieldRepo{db: db}
}

// WithTx returns a copy of the repo whose operations run inside tx.
func (r *CustomFieldRepo) WithTx(tx *sql.Tx) *CustomFieldRepo {
	return &CustomFieldRepo{db: tx}
}

// CreateDef inserts a definition and returns its id.
func (r *CustomFieldRepo) CreateDef(d domain.CustomFieldDef) (int64, error) {
	res, err := r.db.Exec(
		`INSERT INTO custom_field_definitions (entity_type, name, label, kind, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		d.EntityType, d.Name, d.Label, string(d.Kind), d.CreatedAt,
	)
	if err != nil {
		// This INSERT supplies no primary key; the only UNIQUE constraint is
		// (entity_type, name). Do not misclassify other write failures as duplicates.
		const sqliteConstraintUnique = 2067
		var coded interface{ Code() int }
		if errors.As(err, &coded) && coded.Code() == sqliteConstraintUnique {
			return 0, &ConstraintError{Reason: fmt.Sprintf("a field named %q already exists for %s", d.Name, d.EntityType)}
		}
		return 0, fmt.Errorf("insert custom field def: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("custom field def id: %w", err)
	}
	return id, nil
}

// UpdateDefLabel changes only the label of a definition. ErrNotFound if absent.
func (r *CustomFieldRepo) UpdateDefLabel(id int64, label string) error {
	res, err := r.db.Exec(
		`UPDATE custom_field_definitions SET label = ? WHERE id = ?`, label, id,
	)
	if err != nil {
		return fmt.Errorf("update custom field def: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}

// DeleteDef removes a definition and all of its values in one transaction.
// It uses the repo's own db handle; when already inside a tx (via WithTx) it
// runs on that tx, otherwise it opens its own.
func (r *CustomFieldRepo) DeleteDef(id int64) error {
	del := func(q DBTX) error {
		if _, err := q.Exec(`DELETE FROM custom_field_values WHERE def_id = ?`, id); err != nil {
			return fmt.Errorf("delete custom field values: %w", err)
		}
		if _, err := q.Exec(`DELETE FROM custom_field_definitions WHERE id = ?`, id); err != nil {
			return fmt.Errorf("delete custom field def: %w", err)
		}
		return nil
	}
	if db, ok := r.db.(*sql.DB); ok {
		return WithTx(db, func(tx *sql.Tx) error { return del(tx) })
	}
	return del(r.db)
}

// ListDefs returns the definitions for entityType, ordered by id.
func (r *CustomFieldRepo) ListDefs(entityType string) ([]domain.CustomFieldDef, error) {
	return r.queryDefs(
		`SELECT id, entity_type, name, label, kind, created_at
		 FROM custom_field_definitions WHERE entity_type = ? ORDER BY id`,
		entityType,
	)
}

// ListAllDefs returns every definition, ordered by entity_type then id.
func (r *CustomFieldRepo) ListAllDefs() ([]domain.CustomFieldDef, error) {
	return r.queryDefs(
		`SELECT id, entity_type, name, label, kind, created_at
		 FROM custom_field_definitions ORDER BY entity_type, id`,
	)
}

func (r *CustomFieldRepo) queryDefs(sqlStr string, args ...any) ([]domain.CustomFieldDef, error) {
	rows, err := r.db.Query(sqlStr, args...)
	if err != nil {
		return nil, fmt.Errorf("query custom field defs: %w", err)
	}
	defer rows.Close()
	defs := []domain.CustomFieldDef{}
	for rows.Next() {
		var d domain.CustomFieldDef
		var kind string
		if err := rows.Scan(&d.ID, &d.EntityType, &d.Name, &d.Label, &kind, &d.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan custom field def: %w", err)
		}
		d.Kind = domain.CustomFieldKind(kind)
		defs = append(defs, d)
	}
	return defs, rows.Err()
}

// ListForEntity returns the SET values for (entityType, entityID), joined to
// their definitions for name/label/kind, ordered by def id.
func (r *CustomFieldRepo) ListForEntity(entityType string, entityID int64) ([]domain.CustomFieldValue, error) {
	rows, err := r.db.Query(
		`SELECT d.id, d.name, d.label, d.kind, v.value
		 FROM custom_field_values v
		 JOIN custom_field_definitions d ON d.id = v.def_id
		 WHERE v.entity_type = ? AND v.entity_id = ?
		 ORDER BY d.id`,
		entityType, entityID,
	)
	if err != nil {
		return nil, fmt.Errorf("query custom field values: %w", err)
	}
	defer rows.Close()
	vals := []domain.CustomFieldValue{}
	for rows.Next() {
		var v domain.CustomFieldValue
		var kind string
		if err := rows.Scan(&v.DefID, &v.Name, &v.Label, &kind, &v.Value); err != nil {
			return nil, fmt.Errorf("scan custom field value: %w", err)
		}
		v.Kind = domain.CustomFieldKind(kind)
		vals = append(vals, v)
	}
	return vals, rows.Err()
}

// SetForEntity upserts each non-empty value and deletes each empty one for
// (entityType, entityID). Definitions are rechecked under the same write lock:
// form/API parsing can race with definition deletion or inventory replacement.
// A nil map is a no-op.
func (r *CustomFieldRepo) SetForEntity(entityType string, entityID int64, values map[int64]string) error {
	if len(values) == 0 {
		return nil
	}
	if db, ok := r.db.(*sql.DB); ok {
		return WithTx(db, func(tx *sql.Tx) error {
			return r.WithTx(tx).SetForEntity(entityType, entityID, values)
		})
	}
	defs, err := r.ListDefs(entityType)
	if err != nil {
		return err
	}
	byID := make(map[int64]domain.CustomFieldDef, len(defs))
	for _, def := range defs {
		byID[def.ID] = def
	}
	validated := make(map[int64]string, len(values))
	for id, raw := range values {
		def, ok := byID[id]
		if !ok {
			return &ConstraintError{Reason: fmt.Sprintf("custom field %d no longer exists for %s; reload the form and retry", id, entityType)}
		}
		if raw == "" {
			validated[id] = "" // Explicit deletion also applies to boolean fields.
			continue
		}
		value, err := domain.ValidateCustomFieldValue(def.Kind, raw)
		if err != nil {
			return &ConstraintError{Reason: fmt.Sprintf("%s: %v", def.Label, err)}
		}
		validated[id] = value
	}
	for defID, val := range validated {
		if val == "" {
			if _, err := r.db.Exec(
				`DELETE FROM custom_field_values WHERE entity_type = ? AND entity_id = ? AND def_id = ?`,
				entityType, entityID, defID,
			); err != nil {
				return fmt.Errorf("delete custom field value: %w", err)
			}
			continue
		}
		if _, err := r.db.Exec(
			`INSERT INTO custom_field_values (entity_type, entity_id, def_id, value)
			 VALUES (?, ?, ?, ?)
			 ON CONFLICT(entity_type, entity_id, def_id) DO UPDATE SET value = excluded.value`,
			entityType, entityID, defID, val,
		); err != nil {
			return fmt.Errorf("upsert custom field value: %w", err)
		}
	}
	return nil
}

// DeleteByEntity removes every value attached to (entityType, id). Used to clean
// up when an entity is deleted.
func (r *CustomFieldRepo) DeleteByEntity(entityType string, id int64) error {
	if _, err := r.db.Exec(
		`DELETE FROM custom_field_values WHERE entity_type = ? AND entity_id = ?`,
		entityType, id,
	); err != nil {
		return fmt.Errorf("delete custom field values for entity: %w", err)
	}
	return nil
}

// ValuesForEntities bulk-loads the set values for the given entity ids of one
// type, joined to their definitions, keyed by entity id. Avoids the N+1 that a
// per-entity ListForEntity loop would cause in search/list. Empty ids returns an
// empty map without querying.
func (r *CustomFieldRepo) ValuesForEntities(entityType string, ids []int64) (map[int64][]domain.CustomFieldValue, error) {
	out := map[int64][]domain.CustomFieldValue{}
	if len(ids) == 0 {
		return out, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(ids)+1)
	args = append(args, entityType)
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, id)
	}
	rows, err := r.db.Query(
		`SELECT v.entity_id, d.id, d.name, d.label, d.kind, v.value
		 FROM custom_field_values v
		 JOIN custom_field_definitions d ON d.id = v.def_id
		 WHERE v.entity_type = ? AND v.entity_id IN (`+strings.Join(placeholders, ",")+`)
		 ORDER BY v.entity_id, d.id`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("bulk query custom field values: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var entityID int64
		var v domain.CustomFieldValue
		var kind string
		if err := rows.Scan(&entityID, &v.DefID, &v.Name, &v.Label, &kind, &v.Value); err != nil {
			return nil, fmt.Errorf("scan custom field value: %w", err)
		}
		v.Kind = domain.CustomFieldKind(kind)
		out[entityID] = append(out[entityID], v)
	}
	return out, rows.Err()
}

// ListAllValues returns every raw value row, ordered by id (for export).
func (r *CustomFieldRepo) ListAllValues() ([]domain.CustomFieldValueRow, error) {
	rows, err := r.db.Query(
		`SELECT id, entity_type, entity_id, def_id, value
		 FROM custom_field_values ORDER BY id`,
	)
	if err != nil {
		return nil, fmt.Errorf("query custom field value rows: %w", err)
	}
	defer rows.Close()
	out := []domain.CustomFieldValueRow{}
	for rows.Next() {
		var row domain.CustomFieldValueRow
		if err := rows.Scan(&row.ID, &row.EntityType, &row.EntityID, &row.DefID, &row.Value); err != nil {
			return nil, fmt.Errorf("scan custom field value row: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
