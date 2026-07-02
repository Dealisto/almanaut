package store

import (
	"database/sql"
	"fmt"

	"github.com/Dealisto/almanaut/internal/domain"
)

// SubscriptionRepo persists Subscription entities in SQLite.
type SubscriptionRepo struct {
	db DBTX
}

// NewSubscriptionRepo returns a SubscriptionRepo backed by db.
func NewSubscriptionRepo(db *sql.DB) *SubscriptionRepo {
	return &SubscriptionRepo{db: db}
}

// WithTx returns a copy of the repo whose operations run inside tx.
func (r *SubscriptionRepo) WithTx(tx *sql.Tx) *SubscriptionRepo {
	return &SubscriptionRepo{db: tx}
}

// DeleteTx removes the subscription with the given id within tx.
func (r *SubscriptionRepo) DeleteTx(tx *sql.Tx, id int64) error {
	return r.WithTx(tx).Delete(id)
}

// CreateTx inserts s within tx and returns its new id.
func (r *SubscriptionRepo) CreateTx(tx *sql.Tx, s domain.Subscription) (int64, error) {
	return r.WithTx(tx).Create(s)
}

// UpdateTx overwrites the subscription with s.ID within tx.
func (r *SubscriptionRepo) UpdateTx(tx *sql.Tx, s domain.Subscription) error {
	return r.WithTx(tx).Update(s)
}

// GetTx returns the subscription with the given id within tx.
func (r *SubscriptionRepo) GetTx(tx *sql.Tx, id int64) (domain.Subscription, error) {
	return r.WithTx(tx).Get(id)
}

const subscriptionColumns = `id, name, kind, provider, amount, currency, billing_cycle, renewal_date, auto_renew, status, notes`

// Create inserts s and returns its new ID.
func (r *SubscriptionRepo) Create(s domain.Subscription) (int64, error) {
	res, err := r.db.Exec(
		`INSERT INTO subscriptions (name, kind, provider, amount, currency, billing_cycle, renewal_date, auto_renew, status, notes)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.Name, s.Kind, s.Provider, s.Amount, s.Currency, s.BillingCycle, s.RenewalDate, boolToInt(s.AutoRenew), s.Status, s.Notes,
	)
	if err != nil {
		return 0, fmt.Errorf("insert subscription: %w", err)
	}
	return res.LastInsertId()
}

// Get returns the subscription with the given id.
func (r *SubscriptionRepo) Get(id int64) (domain.Subscription, error) {
	row := r.db.QueryRow(`SELECT `+subscriptionColumns+` FROM subscriptions WHERE id = ?`, id)
	return scanSubscription(row)
}

// List returns all subscriptions ordered by name.
func (r *SubscriptionRepo) List() ([]domain.Subscription, error) {
	rows, err := r.db.Query(`SELECT ` + subscriptionColumns + ` FROM subscriptions ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("query subscriptions: %w", err)
	}
	defer rows.Close()
	subs := []domain.Subscription{}
	for rows.Next() {
		s, err := scanSubscription(rows)
		if err != nil {
			return nil, err
		}
		subs = append(subs, s)
	}
	return subs, rows.Err()
}

// Update overwrites the subscription with s.ID with the values in s.
func (r *SubscriptionRepo) Update(s domain.Subscription) error {
	res, err := r.db.Exec(
		`UPDATE subscriptions SET name=?, kind=?, provider=?, amount=?, currency=?, billing_cycle=?, renewal_date=?, auto_renew=?, status=?, notes=? WHERE id=?`,
		s.Name, s.Kind, s.Provider, s.Amount, s.Currency, s.BillingCycle, s.RenewalDate, boolToInt(s.AutoRenew), s.Status, s.Notes, s.ID,
	)
	if err != nil {
		return fmt.Errorf("update subscription: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}

// Delete removes the subscription with the given id.
func (r *SubscriptionRepo) Delete(id int64) error {
	if _, err := r.db.Exec(`DELETE FROM subscriptions WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete subscription: %w", err)
	}
	return nil
}

func scanSubscription(s scanner) (domain.Subscription, error) {
	var sub domain.Subscription
	var autoRenew int64
	if err := s.Scan(&sub.ID, &sub.Name, &sub.Kind, &sub.Provider, &sub.Amount, &sub.Currency, &sub.BillingCycle, &sub.RenewalDate, &autoRenew, &sub.Status, &sub.Notes); err != nil {
		return domain.Subscription{}, notFound(fmt.Errorf("scan subscription: %w", err))
	}
	sub.AutoRenew = autoRenew != 0
	return sub, nil
}
