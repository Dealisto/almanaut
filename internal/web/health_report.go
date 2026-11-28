package web

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

// auditFinding is one offending entity in an audit rule, rendered as a link.
// AckRef, when set to a "type:id" reference, makes the page render an
// "acknowledge" button next to the finding (used by the stale-entity rule).
type auditFinding struct {
	Label  string
	URL    string
	AckRef string
}

// auditRule is one fixed audit rule with its offenders. Count drives both the
// per-rule counter and the dashboard summary.
type auditRule struct {
	ID          string
	Title       string
	Description string
	Findings    []auditFinding
}

// Count is the number of offenders, exposed to the template.
func (r auditRule) Count() int { return len(r.Findings) }

type healthReportData struct {
	Title string
	Rules []auditRule
	Total int
}

// buildAuditRules evaluates configured built-in rules and returns them in display order
// together with the total finding count. It is the single source of truth for
// both the /health-report page and the dashboard summary counter, so the two can
// never disagree.
func buildAuditRules(repos entityRepos, rels *store.RelationshipRepo, cat entityCatalog, changelog *store.ChangelogRepo, staleDays int) ([]auditRule, int, error) {
	settings, err := store.NewHealthRuleSettingsRepo(cat.db).Get()
	if err != nil {
		return nil, 0, err
	}
	if settings.StaleAfterDays != nil {
		staleDays = *settings.StaleAfterDays
	}
	now := time.Now()
	hosts, err := repos.hosts.List()
	if err != nil {
		return nil, 0, err
	}
	services, err := repos.services.List()
	if err != nil {
		return nil, 0, err
	}
	certs, err := repos.certificates.List()
	if err != nil {
		return nil, 0, err
	}
	hardware, err := repos.hardware.List()
	if err != nil {
		return nil, 0, err
	}
	subs, err := repos.subscriptions.List()
	if err != nil {
		return nil, 0, err
	}
	networks, err := repos.networks.List()
	if err != nil {
		return nil, 0, err
	}
	relList, err := rels.List()
	if err != nil {
		return nil, 0, err
	}
	opts, err := cat.options()
	if err != nil {
		return nil, 0, err
	}
	ipamHosts, err := store.IPAMHosts(cat.db)
	if err != nil {
		return nil, 0, err
	}
	ipam := domain.BuildIPAMConflicts(networks, ipamHosts)

	lastActivity, err := changelog.LastActivity()
	if err != nil {
		return nil, 0, err
	}
	staleRefs := domain.StaleRefs(entityRefs(opts), lastActivity, now, staleDays)

	rules := []auditRule{
		{
			ID:          "hosts_without_backup",
			Title:       "Hosts without a backup",
			Description: "Hosts not linked to any backup entity.",
			Findings:    hostFindings(domain.HostsWithoutBackup(hosts, relList), cat),
		},
		{
			ID:          "services_without_host",
			Title:       "Services not linked to a host",
			Description: "Services floating without the host they run on.",
			Findings:    serviceFindings(domain.ServicesWithoutHost(services, relList), cat),
		},
		{
			ID:          "expired_certificates",
			Title:       "Expired certificates",
			Description: "Certificates whose expiry date has already passed.",
			Findings:    certFindings(domain.ExpiredCertificates(certs, now), cat),
		},
		{
			ID:          "unlinked_certificates",
			Title:       "Certificates linked to nothing",
			Description: "Certificates not attached to any entity.",
			Findings:    certFindings(certsLinkedToNothing(certs, relList), cat),
		},
		{
			ID:          "hardware_without_warranty",
			Title:       "Hardware without a warranty date",
			Description: "Hardware whose warranty end is unknown.",
			Findings:    hardwareFindings(domain.HardwareWithoutWarranty(hardware), cat),
		},
		{
			ID:          "subscriptions_without_renewal",
			Title:       "Subscriptions without a renewal date",
			Description: "Subscriptions whose renewal date is unknown.",
			Findings:    subscriptionFindings(domain.SubscriptionsWithoutRenewal(subs), cat),
		},
		{
			ID:          "orphaned_entities",
			Title:       "Orphaned entities",
			Description: "Entities with no relationships at all.",
			// Certificates get their own rule above, so exclude them here to avoid
			// listing an unlinked certificate twice.
			Findings: orphanFindings(opts, relList, cat, "certificate"),
		},
		{
			ID:          "duplicate_ips",
			Title:       "Duplicate IP assignments",
			Description: "The same IP address claimed by multiple inventory owners.",
			Findings:    duplicateIPFindings(ipam.DuplicateIPs, cat),
		},
		{
			ID:          "ips_outside_networks",
			Title:       "IP allocations outside every network",
			Description: "Allocated addresses that fall in no known network.",
			Findings:    allocationFindings(ipam.OutsideNetworks, cat),
		},
		{
			ID:          "overlapping_networks",
			Title:       "Overlapping networks",
			Description: "Networks that occupy the same CIDR block (parent/child subnets excluded).",
			Findings:    overlapFindings(ipam.Overlaps, cat),
		},
		{
			ID:          "stale_entities",
			Title:       "Stale entities",
			Description: staleDescription(staleDays),
			Findings:    staleFindings(opts, staleRefs, cat),
		},
	}

	if settings.RuleEnabled("backup-job-health") {
		backups, err := repos.backups.List()
		if err != nil {
			return nil, 0, err
		}
		latest, err := store.NewBackupRunRepo(cat.db).LatestByBackup(context.Background())
		if err != nil {
			return nil, 0, err
		}
		findings := []auditFinding{}
		for _, backup := range backups {
			var run *domain.BackupRun
			if value, ok := latest[backup.ID]; ok {
				run = &value
			}
			state, message := domain.BackupRunHealth(backup, run, now)
			if state == "never" || state == "failed" || state == "stale" {
				findings = append(findings, auditFinding{Label: backup.Source + " — " + message, URL: cat.path("backup", backup.ID)})
			}
		}
		rules = append(rules, auditRule{ID: "backup-job-health", Title: "Backup job health", Description: "Reporting-enabled backup jobs that have never reported, failed, or exceeded their expected interval.", Findings: findings})
	}
	rules = append(rules, auditRule{ID: "certificates_due_soon", Title: "Certificates expiring soon", Description: fmt.Sprintf("Certificates expiring in the next %d days, excluding those already expired.", settings.CertificateWarningDays), Findings: certFindings(certificatesDueSoon(certs, now, settings.CertificateWarningDays), cat)})
	total := 0
	enabled := make([]auditRule, 0, len(rules))
	for _, r := range rules {
		if !settings.RuleEnabled(r.ID) {
			continue
		}
		enabled = append(enabled, r)
		total += r.Count()
	}
	return enabled, total, nil
}

// healthReport renders the inventory health page: every fixed audit rule with
// its offender count and a drill-down list.
func healthReport(repos entityRepos, rels *store.RelationshipRepo, cat entityCatalog, changelog *store.ChangelogRepo, staleDays int) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		rules, total, err := buildAuditRules(repos, rels, cat, changelog, staleDays)
		if err != nil {
			serverError(w, req, err)
			return
		}
		render(w, req, "health_report.html", healthReportData{
			Title: "Health report",
			Rules: rules,
			Total: total,
		})
	}
}

// acknowledgeStale records an "acknowledge" changelog event for an entity,
// resetting its staleness clock and leaving a trail in the entity's history.
func acknowledgeStale(cat entityCatalog, changelog *store.ChangelogRepo, db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		typ, id, err := parseRef(req.FormValue("ref"))
		if err != nil {
			http.Error(w, "invalid entity reference", http.StatusBadRequest)
			return
		}
		rs, ok := cat.resource(typ)
		if !ok {
			http.Error(w, "unknown entity type", http.StatusBadRequest)
			return
		}
		// Keep the existence check and label lookup under the same write lock
		// as history creation. A concurrent deletion must not leave an
		// acknowledgement for an entity that no longer exists.
		err = store.WithTx(db, func(tx *sql.Tx) error {
			label, err := rs.labelTx(tx, id)
			if err != nil {
				return err
			}
			return changelog.WithTx(tx).Create(store.ChangeEvent{
				EntityType: typ,
				EntityID:   id,
				Label:      label,
				Action:     domain.ActionAcknowledge,
				Actor:      actor(req),
				Changes:    []domain.FieldChange{{Field: "stale", Old: "stale", New: "acknowledged"}},
				CreatedAt:  nowRFC3339(),
			})
		})
		if err != nil {
			notFoundOrServerError(w, req, typ, err)
			return
		}
		http.Redirect(w, req, "/health-report", http.StatusSeeOther)
	}
}

// entityRefs projects catalog options to plain entity references.
func entityRefs(opts []entityOption) []domain.EntityRef {
	out := make([]domain.EntityRef, 0, len(opts))
	for _, o := range opts {
		out = append(out, domain.EntityRef{Type: o.Type, ID: o.ID})
	}
	return out
}

// staleDescription renders the rule's human description with its configured
// window.
func staleDescription(days int) string {
	if days <= 0 {
		return "Stale detection is disabled (set ALMANAUT_STALE_AFTER_DAYS)."
	}
	return fmt.Sprintf("Entities untouched — no edit or discovery sighting — for over %d days. Acknowledge to confirm one is still valid and reset its clock.", days)
}

// staleFindings renders the stale entities, each with an acknowledge button
// (via AckRef), drawn from the catalog option list so labels match the rest of
// the app.
func staleFindings(opts []entityOption, stale []domain.EntityRef, cat entityCatalog) []auditFinding {
	staleSet := make(map[domain.EntityRef]bool, len(stale))
	for _, r := range stale {
		staleSet[r] = true
	}
	out := []auditFinding{}
	for _, o := range opts {
		ref := domain.EntityRef{Type: o.Type, ID: o.ID}
		if staleSet[ref] {
			out = append(out, auditFinding{Label: o.Label, URL: cat.path(o.Type, o.ID), AckRef: o.Value})
		}
	}
	return out
}

// certsLinkedToNothing returns the certificates that appear in no relationship.
func certsLinkedToNothing(certs []domain.Certificate, rels []domain.Relationship) []domain.Certificate {
	linked := domain.LinkedRefs(rels)
	out := []domain.Certificate{}
	for _, c := range certs {
		if !linked[domain.EntityRef{Type: "certificate", ID: c.ID}] {
			out = append(out, c)
		}
	}
	return out
}

func hostFindings(hosts []domain.Host, cat entityCatalog) []auditFinding {
	out := make([]auditFinding, 0, len(hosts))
	for _, h := range hosts {
		out = append(out, auditFinding{Label: h.Name, URL: cat.path("host", h.ID)})
	}
	return out
}

func serviceFindings(services []domain.Service, cat entityCatalog) []auditFinding {
	out := make([]auditFinding, 0, len(services))
	for _, s := range services {
		out = append(out, auditFinding{Label: s.Name, URL: cat.path("service", s.ID)})
	}
	return out
}

func certFindings(certs []domain.Certificate, cat entityCatalog) []auditFinding {
	out := make([]auditFinding, 0, len(certs))
	for _, c := range certs {
		out = append(out, auditFinding{Label: c.Subject, URL: cat.path("certificate", c.ID)})
	}
	return out
}

func hardwareFindings(hw []domain.Hardware, cat entityCatalog) []auditFinding {
	out := make([]auditFinding, 0, len(hw))
	for _, h := range hw {
		out = append(out, auditFinding{Label: h.Name, URL: cat.path("hardware", h.ID)})
	}
	return out
}

func subscriptionFindings(subs []domain.Subscription, cat entityCatalog) []auditFinding {
	out := make([]auditFinding, 0, len(subs))
	for _, s := range subs {
		out = append(out, auditFinding{Label: s.Name, URL: cat.path("subscription", s.ID)})
	}
	return out
}

// duplicateIPFindings lists each colliding host under a shared IP, one finding
// per host so every offending entity is directly linkable.
func duplicateIPFindings(dups []domain.IPConflict, cat entityCatalog) []auditFinding {
	out := []auditFinding{}
	for _, d := range dups {
		for _, h := range d.Hosts {
			out = append(out, auditFinding{
				Label: fmt.Sprintf("%s — %s", d.IP, h.HostName),
				URL:   cat.path(h.Ref().Type, h.Ref().ID),
			})
		}
	}
	return out
}

// allocationFindings renders host IP allocations (used for IPs outside every
// network), linking to the owning host.
func allocationFindings(allocs []domain.Allocation, cat entityCatalog) []auditFinding {
	out := make([]auditFinding, 0, len(allocs))
	for _, a := range allocs {
		out = append(out, auditFinding{
			Label: fmt.Sprintf("%s (%s)", a.HostName, a.IP),
			URL:   cat.path(a.Ref().Type, a.Ref().ID),
		})
	}
	return out
}

// overlapFindings renders each overlapping network pair, linking to the first
// network of the pair.
func overlapFindings(overlaps []domain.NetworkOverlap, cat entityCatalog) []auditFinding {
	out := make([]auditFinding, 0, len(overlaps))
	for _, o := range overlaps {
		out = append(out, auditFinding{
			Label: fmt.Sprintf("%s (%s) ↔ %s (%s)", o.A.Name, o.A.CIDR, o.B.Name, o.B.CIDR),
			URL:   cat.path("network", o.A.ID),
		})
	}
	return out
}

// orphanFindings returns entities that appear in no relationship, drawn from the
// full catalog option list so it stays catalog-driven. Types in exclude are
// skipped because a dedicated rule already covers them.
func orphanFindings(opts []entityOption, rels []domain.Relationship, cat entityCatalog, exclude ...string) []auditFinding {
	skip := map[string]bool{}
	for _, t := range exclude {
		skip[t] = true
	}
	linked := domain.LinkedRefs(rels)
	out := []auditFinding{}
	for _, o := range opts {
		if skip[o.Type] {
			continue
		}
		if linked[domain.EntityRef{Type: o.Type, ID: o.ID}] {
			continue
		}
		out = append(out, auditFinding{Label: o.Label, URL: cat.path(o.Type, o.ID)})
	}
	return out
}

// certificatesDueSoon shares the calendar-date cutoff with existing checks and
// removes the expired subset so the two certificate rules never double count.
func certificatesDueSoon(certs []domain.Certificate, now time.Time, days int) []domain.Certificate {
	expired := make(map[int64]bool)
	for _, cert := range domain.ExpiredCertificates(certs, now) {
		expired[cert.ID] = true
	}
	var due []domain.Certificate
	for _, cert := range domain.ExpiringSoon(certs, now, days) {
		if !expired[cert.ID] {
			due = append(due, cert)
		}
	}
	return due
}
