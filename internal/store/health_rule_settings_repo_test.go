package store

import (
	"database/sql"
	"errors"
	"reflect"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func TestHealthRuleSettingsDefaultsValidationAndReplacement(t *testing.T) {
	db := newTestDB(t)
	repo := NewHealthRuleSettingsRepo(db)
	defaults, err := repo.Get()
	if err != nil || !reflect.DeepEqual(defaults, domain.DefaultHealthRuleSettings()) {
		t.Fatalf("defaults: %+v %v", defaults, err)
	}
	if !defaults.RuleEnabled("hosts_without_backup") || defaults.RuleEnabled("certificates_due_soon") {
		t.Fatal("legacy defaults changed")
	}
	settings := domain.DefaultHealthRuleSettings()
	days := 14
	settings.StaleAfterDays = &days
	settings.CertificateWarningDays = 7
	settings.Enabled = map[string]bool{"hosts_without_backup": false, "certificates_due_soon": true}
	if err := repo.Save(settings); err != nil {
		t.Fatal(err)
	}
	snap, err := Export(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := Import(db, snap); err != nil {
		t.Fatal(err)
	}
	after, err := repo.Get()
	if err != nil || !reflect.DeepEqual(settings, after) {
		t.Fatalf("inventory replacement changed application settings: %+v %v", after, err)
	}
	bad := domain.DefaultHealthRuleSettings()
	bad.Enabled["sql:delete"] = true
	if err := repo.Save(bad); err == nil {
		t.Fatal("accepted unknown rule")
	}
	for _, days := range []int{-1, 36501} {
		bad = domain.DefaultHealthRuleSettings()
		bad.StaleAfterDays = &days
		if err := repo.Save(bad); err == nil {
			t.Fatal("accepted staleness", days)
		}
	}
	for _, days := range []int{0, 3651} {
		bad = domain.DefaultHealthRuleSettings()
		bad.CertificateWarningDays = days
		if err := repo.Save(bad); err == nil {
			t.Fatal("accepted certificate window", days)
		}
	}
	after, _ = repo.Get()
	if !reflect.DeepEqual(settings, after) {
		t.Fatal("invalid save mutated settings")
	}
	rollback := errors.New("rollback")
	if err := WithTx(db, func(tx *sql.Tx) error {
		if err := repo.WithTx(tx).Reset(); err != nil {
			return err
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	after, _ = repo.Get()
	if !reflect.DeepEqual(settings, after) {
		t.Fatal("transaction rollback lost settings")
	}
	if err := repo.Reset(); err != nil {
		t.Fatal(err)
	}
	after, _ = repo.Get()
	if !reflect.DeepEqual(defaults, after) {
		t.Fatal("reset did not restore inherited defaults")
	}
}
