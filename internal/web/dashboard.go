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
	ID        int64
	Name      string
	Href      string
	External  bool
	DetailURL string
	HostName  string
	Liveness  *domain.LivenessStatus
}

type summaryCard struct {
	Label string
	Count int
	URL   string
	Icon  string
	Hint  string
}

type portConnection struct {
	From, To       string
	FromURL, ToURL string
}

type serviceGroup struct {
	Category string
	Services []serviceLink
}

type dashboardData struct {
	Title           string
	Counts          []countCard
	Groups          []attentionGroup
	Services        []serviceGroup
	AnyAttention    bool
	Recent          []historyRow
	Summary         []summaryCard
	Connections     []portConnection
	ConnectionCount int
	Empty           bool
}

// dashboard renders the landing page: per-entity counts and attention groups
// (expiring certs, services without backup, hosts down).
func dashboard(repos entityRepos, rels *store.RelationshipRepo, cat entityCatalog, changelog *store.ChangelogRepo, staleDays int) http.HandlerFunc {
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
		contactCount, err := repos.contacts.Count()
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
		vlanCount, err := repos.vlans.Count()
		if err != nil {
			fail(err)
			return
		}
		reservationCount, err := repos.reservations.Count()
		if err != nil {
			fail(err)
			return
		}
		nicCount, err := repos.nics.Count()
		if err != nil {
			fail(err)
			return
		}
		ports, err := repos.ports.List()
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
			{"Contacts", contactCount, "/contacts"},
			{"Sites", siteCount, "/sites"},
			{"Locations", locationCount, "/locations"},
			{"Racks", rackCount, "/racks"},
			{"VLANs", vlanCount, "/vlans"},
			{"Reservations", reservationCount, "/reservations"},
			{"NICs", nicCount, "/nics"},
			{"Ports", len(ports), "/ports"},
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
		downIDs := make(map[int64]bool, len(down))
		for _, h := range down {
			downIDs[h.ID] = true
		}
		for _, h := range hosts {
			if h.Liveness != nil && h.Liveness.Status == domain.LivenessDown && !downIDs[h.ID] {
				down = append(down, h)
			}
		}
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

		_, healthTotal, err := buildAuditRules(repos, rels, cat, changelog, staleDays)
		if err != nil {
			fail(err)
			return
		}
		var healthItems []attentionItem
		if healthTotal > 0 {
			noun := "issues"
			if healthTotal == 1 {
				noun = "issue"
			}
			healthItems = []attentionItem{{
				Label: fmt.Sprintf("%d inventory %s — view report", healthTotal, noun),
				URL:   "/health-report",
			}}
		}

		groups := []attentionGroup{
			{Title: "Hosts down", Severity: "crit", Items: hostItems},
			{Title: "Inventory health", MoreURL: "/health-report", Severity: "warn", Items: healthItems},
			{Title: "Certificates expiring soon", MoreURL: "/checks", Severity: "warn", Items: certItems},
			{Title: "Services without backup", MoreURL: "/checks", Severity: "warn", Items: svcItems},
			{Title: "Hardware warranty expiring", MoreURL: "/checks", Severity: "warn", Items: hwItems},
			{Title: "Subscriptions renewing soon", MoreURL: "/checks", Severity: "warn", Items: subItems},
		}
		attentionCategories := 0
		for _, group := range groups {
			if len(group.Items) > 0 {
				attentionCategories++
			}
		}
		var hostChecks, serviceChecks []*domain.LivenessStatus
		hostNames := make(map[int64]string, len(hosts))
		for _, host := range hosts {
			hostChecks = append(hostChecks, host.Liveness)
			hostNames[host.ID] = host.Name
		}
		for _, service := range services {
			serviceChecks = append(serviceChecks, service.Liveness)
		}
		serviceHosts := make(map[int64]string)
		for _, rel := range relList {
			if rel.Kind == "runs on" && rel.FromType == "service" && rel.ToType == "host" {
				if name := hostNames[rel.ToID]; name != "" {
					if serviceHosts[rel.FromID] != "" {
						serviceHosts[rel.FromID] += ", "
					}
					serviceHosts[rel.FromID] += name
				}
			}
		}
		serviceGroups := groupServices(services)
		for i := range serviceGroups {
			for j := range serviceGroups[i].Services {
				service := &serviceGroups[i].Services[j]
				service.DetailURL = cat.path("service", service.ID)
				service.HostName = serviceHosts[service.ID]
			}
		}
		connections := buildPortConnections(ports, cat)
		connectionCount := len(connections)
		if len(connections) > 3 {
			connections = connections[:3]
		}
		empty := true
		for _, count := range counts {
			if count.Count > 0 {
				empty = false
				break
			}
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
			Services:     serviceGroups,
			AnyAttention: anyAttention(groups),
			Recent:       recent,
			Summary: []summaryCard{
				{"Hosts", len(hosts), "/hosts", "server", checkSummary(hostChecks)},
				{"Services", len(services), "/services", "layers", checkSummary(serviceChecks)},
				{"Networks", networkCount, "/networks", "network", "Subnets & IP allocations"},
				{"Needs attention", attentionCategories, "/#attention", "alert", "Categories to review"},
			},
			Connections:     connections,
			ConnectionCount: connectionCount,
			Empty:           empty,
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
		link := serviceLink{ID: s.ID, Name: s.Name, Liveness: s.Liveness}
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

// checkSummary reports observations, never inferring availability from a
// missing probe or a manually maintained inventory status.
func checkSummary(checks []*domain.LivenessStatus) string {
	up, down := 0, 0
	for _, check := range checks {
		if check == nil {
			continue
		}
		switch check.Status {
		case domain.LivenessUp:
			up++
		case domain.LivenessDown:
			down++
		}
	}
	if up+down == 0 {
		return "No liveness results yet"
	}
	return fmt.Sprintf("%d up · %d down · %d not checked", up, down, len(checks)-up-down)
}

// buildPortConnections shows each recorded cable once, with links to both
// endpoint records. Dangling peers do not become invented connections.
func buildPortConnections(ports []domain.Port, cat entityCatalog) []portConnection {
	byID := make(map[int64]domain.Port, len(ports))
	for _, port := range ports {
		byID[port.ID] = port
	}
	var connections []portConnection
	for _, port := range ports {
		peer, ok := byID[port.PeerPortID]
		if !ok || port.ID >= peer.ID {
			continue
		}
		connections = append(connections, portConnection{
			From:    port.OwnerName + " / " + port.Name,
			To:      peer.OwnerName + " / " + peer.Name,
			FromURL: cat.path("port", port.ID),
			ToURL:   cat.path("port", peer.ID),
		})
	}
	return connections
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
