package store

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/Dealisto/almanaut/internal/domain"
)

// WebhookRepo persists outbound webhook endpoints in SQLite. The entity_types
// and events filter columns are stored as comma-separated text.
type WebhookRepo struct{ db DBTX }

func NewWebhookRepo(db *sql.DB) *WebhookRepo          { return &WebhookRepo{db: db} }
func (r *WebhookRepo) WithTx(tx *sql.Tx) *WebhookRepo { return &WebhookRepo{db: tx} }

const webhookCols = `id, url, secret, enabled, entity_types, events, created_at`

func (r *WebhookRepo) Create(w domain.Webhook) (int64, error) {
	res, err := r.db.Exec(
		`INSERT INTO webhooks (url, secret, enabled, entity_types, events, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		w.URL, w.Secret, boolToInt(w.Enabled), whJoin(w.EntityTypes), whJoin(w.Events), w.CreatedAt,
	)
	if err != nil {
		return 0, fmt.Errorf("insert webhook: %w", err)
	}
	return res.LastInsertId()
}

func (r *WebhookRepo) Get(id int64) (domain.Webhook, error) {
	row := r.db.QueryRow(`SELECT `+webhookCols+` FROM webhooks WHERE id = ?`, id)
	return scanWebhook(row)
}

func (r *WebhookRepo) List() ([]domain.Webhook, error) {
	return r.query(`SELECT ` + webhookCols + ` FROM webhooks ORDER BY id`)
}

func (r *WebhookRepo) ListEnabled() ([]domain.Webhook, error) {
	return r.query(`SELECT ` + webhookCols + ` FROM webhooks WHERE enabled = 1 ORDER BY id`)
}

func (r *WebhookRepo) query(sqlStr string) ([]domain.Webhook, error) {
	rows, err := r.db.Query(sqlStr)
	if err != nil {
		return nil, fmt.Errorf("query webhooks: %w", err)
	}
	defer rows.Close()
	hooks := []domain.Webhook{}
	for rows.Next() {
		w, err := scanWebhook(rows)
		if err != nil {
			return nil, err
		}
		hooks = append(hooks, w)
	}
	return hooks, rows.Err()
}

func (r *WebhookRepo) Update(w domain.Webhook) error {
	res, err := r.db.Exec(
		`UPDATE webhooks SET url=?, secret=?, enabled=?, entity_types=?, events=? WHERE id=?`,
		w.URL, w.Secret, boolToInt(w.Enabled), whJoin(w.EntityTypes), whJoin(w.Events), w.ID,
	)
	if err != nil {
		return fmt.Errorf("update webhook: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}

func (r *WebhookRepo) Delete(id int64) error {
	if _, err := r.db.Exec(`DELETE FROM webhooks WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete webhook: %w", err)
	}
	return nil
}

func scanWebhook(s scanner) (domain.Webhook, error) {
	var (
		w             domain.Webhook
		enabled       int
		types, events string
	)
	if err := s.Scan(&w.ID, &w.URL, &w.Secret, &enabled, &types, &events, &w.CreatedAt); err != nil {
		return domain.Webhook{}, notFound(fmt.Errorf("scan webhook: %w", err))
	}
	w.Enabled = enabled != 0
	w.EntityTypes = whSplit(types)
	w.Events = whSplit(events)
	return w, nil
}

func whJoin(items []string) string { return strings.Join(items, ",") }

func whSplit(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
