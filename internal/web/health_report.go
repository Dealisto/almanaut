package web

import (
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

// buildAuditRules runs every fixed audit rule and returns them in display order
// together with the total finding count. It is the single source of truth for
// both the /health-report page and the dashboard summary counter, so the two can
// never disagree.
func buildAuditRules(repos entityRepos, rels *store.RelationshipRepo, cat entityCatalog, changelog *store.ChangelogRepo, staleDays int) ([]auditRule, int, error) {
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
	ipam := domain.BuildIPAMConflicts(networks, hosts)

	lastActivity, err := changelog.LastActivity()
	if err != nil {
		return nil, 0, err
	}
	staleRefs := domain.StaleRefs(entityRefs(opts), lastActivity, time.Now(), staleDays)

	rules := []auditRule{
		{
			Title:       "Hosts without a backup",
			Description: "Hosts not linked to any backup entity.",
			Findings:    hostFindings(domain.HostsWithoutBackup(hosts, relList), cat),
		},
		{
			Title:       "Services not linked to a host",
			Description: "Services floating without the host they run on.",
			Findings:    serviceFindings(domain.ServicesWithoutHost(services, relList), cat),
		},
		{
			Title:       "Expired certificates",
			Description: "Certificates whose expiry date has already passed.",
			Findings:    certFindings(domain.ExpiredCertificates(certs, time.Now()), cat),
		},
		{
			Title:       "Certificates linked to nothing",
			Description: "Certificates not attached to any entity.",
			Findings:    certFindings(certsLinkedToNothing(certs, relList), cat),
		},
		{
			Title:       "Hardware without a warranty date",
			Description: "Hardware whose warranty end is unknown.",
			Findings:    hardwareFindings(domain.HardwareWithoutWarranty(hardware), cat),
		},
		{
			Title:       "Subscriptions without a renewal date",
			Description: "Subscriptions whose renewal date is unknown.",
			Findings:    subscriptionFindings(domain.SubscriptionsWithoutRenewal(subs), cat),
		},
		{
			Title:       "Orphaned entities",
			Description: "Entities with no relationships at all.",
			// Certificates get their own rule above, so exclude them here to avoid
			// listing an unlinked certificate twice.
			Findings: orphanFindings(opts, relList, cat, "certificate"),
		},
		{
			Title:       "Duplicate IP assignments",
			Description: "The same IP address claimed by more than one host.",
			Findings:    duplicateIPFindings(ipam.DuplicateIPs, cat),
		},
		{
			Title:       "Host IPs outside every network",
			Description: "Host addresses that fall in no known network.",
			Findings:    allocationFindings(ipam.OutsideNetworks, cat),
		},
		{
			Title:       "Overlapping networks",
			Description: "Networks that occupy the same CIDR block (parent/child subnets excluded).",
			Findings:    overlapFindings(ipam.Overlaps, cat),
		},
		{
			Title:       "Stale entities",
			Description: staleDescription(staleDays),
			Findings:    staleFindings(opts, staleRefs, cat),
		},
	}

	total := 0
	for _, r := range rules {
		total += r.Count()
	}
	return rules, total, nil
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
func acknowledgeStale(cat entityCatalog, changelog *store.ChangelogRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		typ, id, err := parseRef(req.FormValue("ref"))
		if err != nil {
			http.Error(w, "invalid entity reference", http.StatusBadRequest)
			return
		}
		if _, ok := cat.resource(typ); !ok {
			http.Error(w, "unknown entity type", http.StatusBadRequest)
			return
		}
		// Only acknowledge an entity that actually exists, so a stray ref can't
		// append a phantom row to the append-only changelog.
		opts, err := cat.options()
		if err != nil {
			serverError(w, req, err)
			return
		}
		labels := labelMap(opts)
		label, ok := labels[fmt.Sprintf("%s:%d", typ, id)]
		if !ok {
			http.NotFound(w, req)
			return
		}
		if err := changelog.Create(store.ChangeEvent{
			EntityType: typ,
			EntityID:   id,
			Label:      label,
			Action:     domain.ActionAcknowledge,
			Actor:      actor(req),
			Changes:    []domain.FieldChange{{Field: "stale", Old: "stale", New: "acknowledged"}},
			CreatedAt:  nowRFC3339(),
		}); err != nil {
			serverError(w, req, err)
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
				URL:   cat.path("host", h.HostID),
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
			URL:   cat.path("host", a.HostID),
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
