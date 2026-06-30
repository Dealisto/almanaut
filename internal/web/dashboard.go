package web

import (
	"fmt"
	"net/http"
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
	Title   string
	MoreURL string
	Items   []attentionItem
}

type dashboardData struct {
	Title  string
	Counts []countCard
	Groups []attentionGroup
}

// dashboard renders the landing page: per-entity counts and attention groups
// (expiring certs, services without backup, hosts down).
func dashboard(repos entityRepos, rels *store.RelationshipRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		fail := func(err error) { http.Error(w, err.Error(), http.StatusInternalServerError) }

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
		networks, err := repos.networks.List()
		if err != nil {
			fail(err)
			return
		}
		domains, err := repos.domains.List()
		if err != nil {
			fail(err)
			return
		}
		certs, err := repos.certificates.List()
		if err != nil {
			fail(err)
			return
		}
		backups, err := repos.backups.List()
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
		accounts, err := repos.accounts.List()
		if err != nil {
			fail(err)
			return
		}
		relList, err := rels.List()
		if err != nil {
			fail(err)
			return
		}

		counts := []countCard{
			{"Hosts", len(hosts), "/hosts"},
			{"Services", len(services), "/services"},
			{"Networks", len(networks), "/networks"},
			{"Domains", len(domains), "/domains"},
			{"Certificates", len(certs), "/certificates"},
			{"Backups", len(backups), "/backups"},
			{"Hardware", len(hardware), "/hardware"},
			{"Subscriptions", len(subscriptions), "/subscriptions"},
			{"Accounts", len(accounts), "/accounts"},
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

		render(w, req, "dashboard.html", dashboardData{
			Title:  "Dashboard",
			Counts: counts,
			Groups: []attentionGroup{
				{Title: "Certificates expiring soon", MoreURL: "/checks", Items: certItems},
				{Title: "Services without backup", MoreURL: "/checks", Items: svcItems},
				{Title: "Hosts down", Items: hostItems},
				{Title: "Hardware warranty expiring", MoreURL: "/checks", Items: hwItems},
				{Title: "Subscriptions renewing soon", MoreURL: "/checks", Items: subItems},
			},
		})
	}
}
