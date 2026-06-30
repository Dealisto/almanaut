package web

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/Dealisto/almanaut/internal/store"
)

// searchHit is one matching entity on the search results page.
type searchHit struct {
	Label string
	URL   string
}

// searchGroup is the matches for one entity type.
type searchGroup struct {
	Heading string
	Hits    []searchHit
}

// searchPageData backs search.html.
type searchPageData struct {
	Title  string
	Query  string
	Groups []searchGroup
	Total  int
}

// matchesQuery reports whether any field contains q, case-insensitively.
func matchesQuery(fields []string, q string) bool {
	lq := strings.ToLower(q)
	for _, f := range fields {
		if strings.Contains(strings.ToLower(f), lq) {
			return true
		}
	}
	return false
}

// searchEntities renders the global search results for ?q=…: every entity
// whose searchable fields contain the query, plus entities carrying a matching
// tag, grouped by type and linked to their detail pages.
func searchEntities(
	hostRepo *store.HostRepo, serviceRepo *store.ServiceRepo, networkRepo *store.NetworkRepo,
	domainRepo *store.DomainRepo, certRepo *store.CertificateRepo, backupRepo *store.BackupRepo,
	hardwareRepo *store.HardwareRepo, subscriptionRepo *store.SubscriptionRepo, accountRepo *store.AccountRepo,
	tags *store.TagRepo,
) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		q := strings.TrimSpace(req.URL.Query().Get("q"))
		data := searchPageData{Title: "Search", Query: q}
		if q == "" {
			render(w, req, "search.html", data)
			return
		}

		type bucket struct {
			typ, path, heading string
			hits               []searchHit
		}
		hosts := &bucket{typ: "host", path: "/hosts", heading: "Hosts"}
		services := &bucket{typ: "service", path: "/services", heading: "Services"}
		networks := &bucket{typ: "network", path: "/networks", heading: "Networks"}
		domains := &bucket{typ: "domain", path: "/domains", heading: "Domains"}
		certificates := &bucket{typ: "certificate", path: "/certificates", heading: "Certificates"}
		backups := &bucket{typ: "backup", path: "/backups", heading: "Backups"}
		hardware := &bucket{typ: "hardware", path: "/hardware", heading: "Hardware"}
		subscriptions := &bucket{typ: "subscription", path: "/subscriptions", heading: "Subscriptions"}
		accounts := &bucket{typ: "account", path: "/accounts", heading: "Accounts"}
		buckets := map[string]*bucket{
			"host": hosts, "service": services, "network": networks,
			"domain": domains, "certificate": certificates, "backup": backups,
			"hardware": hardware, "subscription": subscriptions, "account": accounts,
		}

		seen := map[string]bool{}      // dedupe by "type:id"
		labelOf := map[string]string{} // "type:id" -> primary label (for tag hits)
		add := func(b *bucket, id int64, label string) {
			key := fmt.Sprintf("%s:%d", b.typ, id)
			if seen[key] {
				return
			}
			seen[key] = true
			b.hits = append(b.hits, searchHit{Label: label, URL: fmt.Sprintf("%s/%d", b.path, id)})
		}
		fail := func(err error) {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

		hostList, err := hostRepo.List()
		if err != nil {
			fail(err)
			return
		}
		for _, h := range hostList {
			labelOf[fmt.Sprintf("host:%d", h.ID)] = h.Name
			if matchesQuery([]string{h.Name, h.OS, h.CPU, h.RAM, h.Disk, h.Status, h.Notes, strings.Join(h.IPs, " ")}, q) {
				add(hosts, h.ID, h.Name)
			}
		}
		serviceList, err := serviceRepo.List()
		if err != nil {
			fail(err)
			return
		}
		for _, s := range serviceList {
			labelOf[fmt.Sprintf("service:%d", s.ID)] = s.Name
			if matchesQuery([]string{s.Name, s.Kind, s.URL, s.Ports, s.Category, s.Notes}, q) {
				add(services, s.ID, s.Name)
			}
		}
		networkList, err := networkRepo.List()
		if err != nil {
			fail(err)
			return
		}
		for _, n := range networkList {
			labelOf[fmt.Sprintf("network:%d", n.ID)] = n.Name
			if matchesQuery([]string{n.Name, n.CIDR, n.VLAN, n.Gateway, n.Notes}, q) {
				add(networks, n.ID, n.Name)
			}
		}
		domainList, err := domainRepo.List()
		if err != nil {
			fail(err)
			return
		}
		for _, d := range domainList {
			labelOf[fmt.Sprintf("domain:%d", d.ID)] = d.FQDN
			if matchesQuery([]string{d.FQDN, d.Provider, d.Notes}, q) {
				add(domains, d.ID, d.FQDN)
			}
		}
		certList, err := certRepo.List()
		if err != nil {
			fail(err)
			return
		}
		for _, c := range certList {
			labelOf[fmt.Sprintf("certificate:%d", c.ID)] = c.Subject
			if matchesQuery([]string{c.Subject, c.Issuer, c.Notes}, q) {
				add(certificates, c.ID, c.Subject)
			}
		}
		backupList, err := backupRepo.List()
		if err != nil {
			fail(err)
			return
		}
		for _, b := range backupList {
			labelOf[fmt.Sprintf("backup:%d", b.ID)] = b.Source
			if matchesQuery([]string{b.Source, b.Destination, b.Frequency, b.LastRun, b.Notes}, q) {
				add(backups, b.ID, b.Source)
			}
		}
		hardwareList, err := hardwareRepo.List()
		if err != nil {
			fail(err)
			return
		}
		for _, h := range hardwareList {
			labelOf[fmt.Sprintf("hardware:%d", h.ID)] = h.Name
			if matchesQuery([]string{h.Name, h.Kind, h.Manufacturer, h.Model, h.Serial, h.Location, h.Status, h.Notes}, q) {
				add(hardware, h.ID, h.Name)
			}
		}

		subscriptionList, err := subscriptionRepo.List()
		if err != nil {
			fail(err)
			return
		}
		for _, s := range subscriptionList {
			labelOf[fmt.Sprintf("subscription:%d", s.ID)] = s.Name
			if matchesQuery([]string{s.Name, s.Kind, s.Provider, s.Currency, s.BillingCycle, s.Status, s.Notes}, q) {
				add(subscriptions, s.ID, s.Name)
			}
		}

		accountList, err := accountRepo.List()
		if err != nil {
			fail(err)
			return
		}
		for _, a := range accountList {
			labelOf[fmt.Sprintf("account:%d", a.ID)] = a.Name
			if matchesQuery([]string{a.Name, a.Kind, a.Username, a.PasswordManager, a.SecretRef, a.URL, a.Status, a.Notes}, q) {
				add(accounts, a.ID, a.Name)
			}
		}

		// Fold in tag matches: a matching tag pulls its entity into that group.
		tagged, err := tags.Search(q)
		if err != nil {
			fail(err)
			return
		}
		for _, tg := range tagged {
			b, ok := buckets[tg.EntityType]
			if !ok {
				continue
			}
			label, ok := labelOf[fmt.Sprintf("%s:%d", tg.EntityType, tg.EntityID)]
			if !ok {
				continue // orphan tag (entity deleted) — out of scope, see #28
			}
			add(b, tg.EntityID, label)
		}

		for _, b := range []*bucket{hosts, services, networks, domains, certificates, backups, hardware, subscriptions, accounts} {
			if len(b.hits) == 0 {
				continue
			}
			sort.Slice(b.hits, func(i, j int) bool { return b.hits[i].Label < b.hits[j].Label })
			data.Groups = append(data.Groups, searchGroup{Heading: b.heading, Hits: b.hits})
			data.Total += len(b.hits)
		}
		render(w, req, "search.html", data)
	}
}
