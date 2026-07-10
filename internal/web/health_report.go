package web

import (
	"net/http"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

// auditFinding is one offending entity in an audit rule, rendered as a link.
type auditFinding struct {
	Label string
	URL   string
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
func buildAuditRules(repos entityRepos, rels *store.RelationshipRepo, cat entityCatalog) ([]auditRule, int, error) {
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
	relList, err := rels.List()
	if err != nil {
		return nil, 0, err
	}
	opts, err := cat.options()
	if err != nil {
		return nil, 0, err
	}

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
	}

	total := 0
	for _, r := range rules {
		total += r.Count()
	}
	return rules, total, nil
}

// healthReport renders the inventory health page: every fixed audit rule with
// its offender count and a drill-down list.
func healthReport(repos entityRepos, rels *store.RelationshipRepo, cat entityCatalog) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		rules, total, err := buildAuditRules(repos, rels, cat)
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
