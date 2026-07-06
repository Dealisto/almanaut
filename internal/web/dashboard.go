package web

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

type countCard struct {
	Label string
	Count int
	URL   string
}

type attentionItem struct {
	Label string
	URL   string
}

type attentionGroup struct {
	Title    string
	MoreURL  string
	Severity string // "warn" or "crit" — drives the dashboard severity dot
	Items    []attentionItem
}

type serviceLink struct {
	Name     string
	Href     string
	External bool
}

type serviceGroup struct {
	Category string
	Services []serviceLink
}

type dashboardData struct {
	Title        string
	Counts       []countCard
	Groups       []attentionGroup
	Services     []serviceGroup
	AnyAttention bool
	Recent       []historyRow
}

// dashboard renders the landing page: per-entity counts and attention groups
// (expiring certs, services without backup, hosts down).
func dashboard(repos entityRepos, rels *store.RelationshipRepo, cat entityCatalog, changelog *store.ChangelogRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		fail := func(err error) { serverError(w, req, err) }

		hosts, err := repos.hosts.List()
		if err != nil {
			fail(err)
			return
		}
		services, err := repos.services.List()
		if err != nil {
			fail(err)
			return
		}
		certs, err := repos.certificates.List()
		if err != nil {
			fail(err)
			return
		}
		hardware, err := repos.hardware.List()
		if err != nil {
			fail(err)
			return
		}
		subscriptions, err := repos.subscriptions.List()
		if err != nil {
			fail(err)
			return
		}
		relList, err := rels.List()
		if err != nil {
			fail(err)
			return
		}
		// Networks, domains, backups, and accounts feed only count cards, so a
		// COUNT(*) avoids loading every row just to take its length.
		networkCount, err := repos.networks.Count()
		if err != nil {
			fail(err)
			return
		}
		domainCount, err := repos.domains.Count()
		if err != nil {
			fail(err)
			return
		}
		backupCount, err := repos.backups.Count()
		if err != nil {
			fail(err)
			return
		}
		accountCount, err := repos.accounts.Count()
		if err != nil {
			fail(err)
			return
		}
		siteCount, err := repos.sites.Count()
		if err != nil {
			fail(err)
			return
		}
		locationCount, err := repos.locations.Count()
		if err != nil {
			fail(err)
			return
		}
		rackCount, err := repos.racks.Count()
		if err != nil {
			fail(err)
			return
		}

		counts := []countCard{
			{"Hosts", len(hosts), "/hosts"},
			{"Services", len(services), "/services"},
			{"Networks", networkCount, "/networks"},
			{"Domains", domainCount, "/domains"},
			{"Certificates", len(certs), "/certificates"},
			{"Backups", backupCount, "/backups"},
			{"Hardware", len(hardware), "/hardware"},
			{"Subscriptions", len(subscriptions), "/subscriptions"},
			{"Accounts", accountCount, "/accounts"},
			{"Sites", siteCount, "/sites"},
			{"Locations", locationCount, "/locations"},
			{"Racks", rackCount, "/racks"},
		}

		expiring := domain.ExpiringSoon(certs, time.Now(), 30)
		certItems := make([]attentionItem, 0, len(expiring))
		for _, c := range expiring {
			certItems = append(certItems, attentionItem{
				Label: fmt.Sprintf("%s (%s)", c.Subject, c.ExpiresOn),
				URL:   fmt.Sprintf("/certificates/%d", c.ID),
			})
		}
		unbacked := domain.ServicesWithoutBackup(services, relList)
		svcItems := make([]attentionItem, 0, len(unbacked))
		for _, s := range unbacked {
			svcItems = append(svcItems, attentionItem{Label: s.Name, URL: fmt.Sprintf("/services/%d", s.ID)})
		}
		down := domain.HostsDown(hosts)
		hostItems := make([]attentionItem, 0, len(down))
		for _, h := range down {
			hostItems = append(hostItems, attentionItem{Label: h.Name, URL: fmt.Sprintf("/hosts/%d", h.ID)})
		}
		expiringHW := domain.WarrantyExpiring(hardware, time.Now(), 30)
		hwItems := make([]attentionItem, 0, len(expiringHW))
		for _, h := range expiringHW {
			hwItems = append(hwItems, attentionItem{
				Label: fmt.Sprintf("%s (%s)", h.Name, h.WarrantyEnd),
				URL:   fmt.Sprintf("/hardware/%d", h.ID),
			})
		}
		dueRenewals := domain.RenewalsDue(subscriptions, time.Now(), 30)
		subItems := make([]attentionItem, 0, len(dueRenewals))
		for _, s := range dueRenewals {
			subItems = append(subItems, attentionItem{
				Label: fmt.Sprintf("%s (%s)", s.Name, s.RenewalDate),
				URL:   fmt.Sprintf("/subscriptions/%d", s.ID),
			})
		}

		groups := []attentionGroup{
			{Title: "Certificates expiring soon", MoreURL: "/checks", Severity: "warn", Items: certItems},
			{Title: "Services without backup", MoreURL: "/checks", Severity: "warn", Items: svcItems},
			{Title: "Hosts down", Severity: "crit", Items: hostItems},
			{Title: "Hardware warranty expiring", MoreURL: "/checks", Severity: "warn", Items: hwItems},
			{Title: "Subscriptions renewing soon", MoreURL: "/checks", Severity: "warn", Items: subItems},
		}
		recentEvents, err := changelog.ListRecent(5)
		if err != nil {
			fail(err)
			return
		}
		recent, err := buildActivityRows(cat, recentEvents)
		if err != nil {
			fail(err)
			return
		}
		render(w, req, "dashboard.html", dashboardData{
			Title:        "Dashboard",
			Counts:       counts,
			Groups:       groups,
			Services:     groupServices(services),
			AnyAttention: anyAttention(groups),
			Recent:       recent,
		})
	}
}

// groupServices arranges services into the home launcher: grouped by category
// (blank → "Uncategorized"), groups sorted case-insensitively by category and
// services by name. A service with a URL links out to it (External); one
// without links to its almanaut detail page.
func groupServices(services []domain.Service) []serviceGroup {
	byCat := map[string][]serviceLink{}
	for _, s := range services {
		cat := strings.TrimSpace(s.Category)
		if cat == "" {
			cat = "Uncategorized"
		}
		link := serviceLink{Name: s.Name}
		if u := strings.TrimSpace(s.URL); u != "" {
			link.Href, link.External = u, true
		} else {
			link.Href = fmt.Sprintf("/services/%d", s.ID)
		}
		byCat[cat] = append(byCat[cat], link)
	}
	groups := make([]serviceGroup, 0, len(byCat))
	for cat, links := range byCat {
		sort.Slice(links, func(i, j int) bool {
			return strings.ToLower(links[i].Name) < strings.ToLower(links[j].Name)
		})
		groups = append(groups, serviceGroup{Category: cat, Services: links})
	}
	sort.Slice(groups, func(i, j int) bool {
		return strings.ToLower(groups[i].Category) < strings.ToLower(groups[j].Category)
	})
	return groups
}

// anyAttention reports whether any attention group has items.
func anyAttention(groups []attentionGroup) bool {
	for _, g := range groups {
		if len(g.Items) > 0 {
			return true
		}
	}
	return false
}
