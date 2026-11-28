package web

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

type healthRuleSettingView struct {
	ID, Title string
	Enabled   bool
}
type healthRuleSettingsData struct {
	Title, Error, StaleDays, CertificateDays string
	DefaultStaleDays                         int
	Rules                                    []healthRuleSettingView
}

func healthRuleSettingsView(settings domain.HealthRuleSettings, defaultStale int) healthRuleSettingsData {
	data := healthRuleSettingsData{Title: "Health rules", DefaultStaleDays: defaultStale, CertificateDays: strconv.Itoa(settings.CertificateWarningDays)}
	if settings.StaleAfterDays != nil {
		data.StaleDays = strconv.Itoa(*settings.StaleAfterDays)
	}
	for _, rule := range domain.HealthRuleDefinitions {
		data.Rules = append(data.Rules, healthRuleSettingView{ID: rule.ID, Title: rule.Title, Enabled: settings.RuleEnabled(rule.ID)})
	}
	return data
}

func healthRuleSettingsPage(db *sql.DB, defaultStale int) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		settings, err := store.NewHealthRuleSettingsRepo(db).Get()
		if err != nil {
			serverError(w, req, err)
			return
		}
		render(w, req, "health_rule_settings.html", healthRuleSettingsView(settings, defaultStale))
	}
}

func parseHealthRuleSettings(req *http.Request) (domain.HealthRuleSettings, error) {
	settings := domain.DefaultHealthRuleSettings()
	if err := req.ParseForm(); err != nil {
		return settings, fmt.Errorf("invalid form")
	}
	if req.PostForm.Get("settings_present") != "1" {
		return settings, fmt.Errorf("submit the health rules form to update settings")
	}
	for _, rule := range domain.HealthRuleDefinitions {
		settings.Enabled[rule.ID] = false
	}
	seen := map[string]bool{}
	for _, id := range req.PostForm["enabled"] {
		if seen[id] {
			return settings, fmt.Errorf("duplicate health rule %q", id)
		}
		seen[id] = true
		settings.Enabled[id] = true
	}
	if raw := strings.TrimSpace(req.PostForm.Get("stale_after_days")); raw != "" {
		days, err := strconv.Atoi(raw)
		if err != nil {
			return settings, fmt.Errorf("staleness must be a whole number of days")
		}
		settings.StaleAfterDays = &days
	}
	days, err := strconv.Atoi(strings.TrimSpace(req.PostForm.Get("certificate_warning_days")))
	if err != nil {
		return settings, fmt.Errorf("certificate warning must be a whole number of days")
	}
	settings.CertificateWarningDays = days
	return settings, settings.Validate()
}

func saveHealthRuleSettings(db *sql.DB, defaultStale int) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		settings, err := parseHealthRuleSettings(req)
		if err != nil {
			data := healthRuleSettingsView(settings, defaultStale)
			data.Error = err.Error()
			data.StaleDays = req.PostForm.Get("stale_after_days")
			data.CertificateDays = req.PostForm.Get("certificate_warning_days")
			renderStatus(w, req, "health_rule_settings.html", data, http.StatusBadRequest)
			return
		}
		err = store.WithTx(db, func(tx *sql.Tx) error {
			repo := store.NewHealthRuleSettingsRepo(db).WithTx(tx)
			before, err := repo.Get()
			if err != nil {
				return err
			}
			if err := repo.Save(settings); err != nil {
				return err
			}
			return auditHealthRuleSettings(tx, db, req, "health_rules_updated", before, settings)
		})
		if err != nil {
			serverError(w, req, err)
			return
		}
		http.Redirect(w, req, "/health-rules", http.StatusSeeOther)
	}
}

func resetHealthRuleSettings(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		err := store.WithTx(db, func(tx *sql.Tx) error {
			repo := store.NewHealthRuleSettingsRepo(db).WithTx(tx)
			before, err := repo.Get()
			if err != nil {
				return err
			}
			if err := repo.Reset(); err != nil {
				return err
			}
			return auditHealthRuleSettings(tx, db, req, "health_rules_reset", before, domain.DefaultHealthRuleSettings())
		})
		if err != nil {
			serverError(w, req, err)
			return
		}
		http.Redirect(w, req, "/health-rules", http.StatusSeeOther)
	}
}

func auditHealthRuleSettings(tx *sql.Tx, db *sql.DB, req *http.Request, event string, before, after domain.HealthRuleSettings) error {
	detail, err := json.Marshal(struct{ Before, After domain.HealthRuleSettings }{before, after})
	if err != nil {
		return err
	}
	return store.NewAuthEventRepo(db).WithTx(tx).Create(domain.AuthEvent{Type: event, Username: actor(req), UserID: currentUserID(req.Context()), SourceIP: boundedAuditText(clientIP(req), 64), Detail: string(detail), CreatedAt: nowRFC3339()})
}
