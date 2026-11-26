package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var ErrWebhookBacklog = errors.New("webhook delivery backlog is full")

// WebhookDelivery is an immutable signed request plus its durable retry state.
// LeaseToken fences late completions after another worker reclaims an expired lease.
type WebhookDelivery struct {
	ID, URL, Signature string
	WebhookID          int64
	Body               []byte
	Attempts           int
	LeaseToken         string
}

type WebhookOutboxRepo struct{ db DBTX }

func NewWebhookOutboxRepo(db *sql.DB) *WebhookOutboxRepo { return &WebhookOutboxRepo{db: db} }
func (r *WebhookOutboxRepo) WithTx(tx *sql.Tx) *WebhookOutboxRepo {
	return &WebhookOutboxRepo{db: tx}
}

// Enqueue bounds both retained requests and request-content bytes. On a tx-bound repo
// admission participates in the inventory transaction; failure must roll it back.
func (r *WebhookOutboxRepo) Enqueue(d WebhookDelivery, due time.Time, capacity int, byteLimit int64) error {
	if db, ok := r.db.(*sql.DB); ok {
		return WithTx(db, func(tx *sql.Tx) error { return r.WithTx(tx).Enqueue(d, due, capacity, byteLimit) })
	}
	var count int
	var bytes int64
	if err := r.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(length(body) + length(CAST(url AS BLOB)) + length(signature) + length(delivery_id)), 0) FROM webhook_outbox`).Scan(&count, &bytes); err != nil {
		return fmt.Errorf("read webhook backlog: %w", err)
	}
	size := int64(len(d.Body)) + int64(len(d.URL)) + int64(len(d.Signature)) + int64(len(d.ID))
	if count >= capacity || size > byteLimit-bytes {
		return ErrWebhookBacklog
	}
	_, err := r.db.Exec(`INSERT INTO webhook_outbox (delivery_id, webhook_id, url, body, signature, due_at) VALUES (?, ?, ?, ?, ?, ?)`,
		d.ID, d.WebhookID, d.URL, d.Body, d.Signature, due.UnixMilli())
	if err != nil {
		return fmt.Errorf("enqueue webhook delivery: %w", err)
	}
	return nil
}

func (r *WebhookOutboxRepo) PendingCount() (int, error) {
	var count int
	err := r.db.QueryRow(`SELECT COUNT(*) FROM webhook_outbox`).Scan(&count)
	return count, err
}

// Claim uses one atomic statement, including across processes. An expired lease
// reuses its interrupted attempt: the previous worker might have crashed before
// sending any bytes. Only a confirmed failure followed by Retry spends another
// attempt. A receiver must still deduplicate an HTTP success lost before its ack.
func (r *WebhookOutboxRepo) Claim(now time.Time, lease time.Duration, token string) (WebhookDelivery, error) {
	var d WebhookDelivery
	err := r.db.QueryRow(`UPDATE webhook_outbox SET lease_token=?, lease_until=?, attempts=attempts+CASE WHEN lease_token='' THEN 1 ELSE 0 END
		WHERE delivery_id=(SELECT delivery_id FROM webhook_outbox WHERE due_at<=? AND lease_until<=? ORDER BY due_at, delivery_id LIMIT 1)
		RETURNING delivery_id, webhook_id, url, body, signature, attempts, lease_token`,
		token, now.Add(lease).UnixMilli(), now.UnixMilli(), now.UnixMilli()).Scan(
		&d.ID, &d.WebhookID, &d.URL, &d.Body, &d.Signature, &d.Attempts, &d.LeaseToken)
	return d, notFound(err)
}

// Active checks endpoint invalidation after claiming, immediately before HTTP.
// An already in-flight HTTP request cannot be recalled by a database update.
func (r *WebhookOutboxRepo) Active(d WebhookDelivery) (bool, error) {
	var active bool
	err := r.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM webhook_outbox WHERE delivery_id=? AND lease_token=?)`, d.ID, d.LeaseToken).Scan(&active)
	return active, err
}

func (r *WebhookOutboxRepo) Complete(d WebhookDelivery) error {
	_, err := r.db.Exec(`DELETE FROM webhook_outbox WHERE delivery_id=? AND lease_token=?`, d.ID, d.LeaseToken)
	return err
}

// Retry releases the lease without occupying a worker during the delay.
// Cancellation refunds the attempt so clean shutdowns do not exhaust retries.
func (r *WebhookOutboxRepo) Retry(d WebhookDelivery, due time.Time, canceled bool) error {
	refund := 0
	if canceled {
		refund = 1
	}
	_, err := r.db.Exec(`UPDATE webhook_outbox SET due_at=?, lease_until=0, lease_token='', attempts=MAX(0, attempts-?) WHERE delivery_id=? AND lease_token=?`,
		due.UnixMilli(), refund, d.ID, d.LeaseToken)
	return err
}
