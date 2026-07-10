package store

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/Dealisto/almanaut/internal/domain"
)

// AuthEventRepo persists the authentication audit log.
type AuthEventRepo struct {
	db DBTX
}

// NewAuthEventRepo returns an AuthEventRepo backed by db.
func NewAuthEventRepo(db *sql.DB) *AuthEventRepo { return &AuthEventRepo{db: db} }

// WithTx returns a copy of the repo whose operations run inside tx.
func (r *AuthEventRepo) WithTx(tx *sql.Tx) *AuthEventRepo { return &AuthEventRepo{db: tx} }

// Create inserts one audit event.
func (r *AuthEventRepo) Create(e domain.AuthEvent) error {
	if _, err := r.db.Exec(
		`INSERT INTO auth_events (event_type, username, user_id, source_ip, detail, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		e.Type, e.Username, e.UserID, e.SourceIP, e.Detail, e.CreatedAt,
	); err != nil {
		return fmt.Errorf("insert auth event: %w", err)
	}
	return nil
}

// AuthEventFilter narrows an audit-log query. Zero-value fields are ignored.
type AuthEventFilter struct {
	Username string // exact match
	Type     string // exact match
	Since    string // created_at >= (RFC3339 or YYYY-MM-DD)
	Until    string // created_at <= (RFC3339 or YYYY-MM-DD)
	Limit    int    // max rows (0 → default 500)
}

// List returns audit events matching filter, newest first.
func (r *AuthEventRepo) List(f AuthEventFilter) ([]domain.AuthEvent, error) {
	var where []string
	var args []any
	if f.Username != "" {
		where = append(where, "username = ?")
		args = append(args, f.Username)
	}
	if f.Type != "" {
		where = append(where, "event_type = ?")
		args = append(args, f.Type)
	}
	if f.Since != "" {
		where = append(where, "created_at >= ?")
		args = append(args, f.Since)
	}
	if f.Until != "" {
		until := f.Until
		if len(until) == len("2006-01-02") { // bare date → include the whole day
			until += "T23:59:59Z"
		}
		where = append(where, "created_at <= ?")
		args = append(args, until)
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 500
	}
	q := `SELECT id, event_type, username, user_id, source_ip, detail, created_at FROM auth_events`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY id DESC LIMIT ?"
	args = append(args, limit)

	rows, err := r.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("query auth events: %w", err)
	}
	defer rows.Close()
	events := []domain.AuthEvent{}
	for rows.Next() {
		var e domain.AuthEvent
		if err := rows.Scan(&e.ID, &e.Type, &e.Username, &e.UserID, &e.SourceIP, &e.Detail, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan auth event: %w", err)
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// Prune deletes events older than cutoff (an RFC3339 timestamp) and returns the
// number removed. Used to enforce the retention window.
func (r *AuthEventRepo) Prune(cutoff string) (int64, error) {
	res, err := r.db.Exec(`DELETE FROM auth_events WHERE created_at < ?`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("prune auth events: %w", err)
	}
	return res.RowsAffected()
}
