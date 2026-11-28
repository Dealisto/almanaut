package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Dealisto/almanaut/internal/domain"
)

// HealthRuleSettingsRepo persists application policy, not inventory records.
type HealthRuleSettingsRepo struct{ db DBTX }

func NewHealthRuleSettingsRepo(db *sql.DB) *HealthRuleSettingsRepo {
	return &HealthRuleSettingsRepo{db: db}
}
func (r *HealthRuleSettingsRepo) WithTx(tx *sql.Tx) *HealthRuleSettingsRepo {
	return &HealthRuleSettingsRepo{db: tx}
}

func (r *HealthRuleSettingsRepo) Get() (domain.HealthRuleSettings, error) {
	settings := domain.DefaultHealthRuleSettings()
	var raw string
	if err := r.db.QueryRow("SELECT settings FROM health_rule_settings WHERE id=1").Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return settings, nil
		}
		return settings, fmt.Errorf("read health rule settings: %w", err)
	}
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		return settings, fmt.Errorf("decode health rule settings: %w", err)
	}
	if err := settings.Validate(); err != nil {
		return settings, fmt.Errorf("invalid saved health rule settings: %w", err)
	}
	return settings, nil
}

func (r *HealthRuleSettingsRepo) Save(settings domain.HealthRuleSettings) error {
	if err := settings.Validate(); err != nil {
		return &ConstraintError{Reason: err.Error()}
	}
	raw, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	if _, err := r.db.Exec("INSERT INTO health_rule_settings(id,settings) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET settings=excluded.settings", string(raw)); err != nil {
		return fmt.Errorf("save health rule settings: %w", err)
	}
	return nil
}
func (r *HealthRuleSettingsRepo) Reset() error {
	_, err := r.db.Exec("DELETE FROM health_rule_settings WHERE id=1")
	return err
}
