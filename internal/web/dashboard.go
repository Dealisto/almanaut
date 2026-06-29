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
func dashboard(
	hostRepo *store.HostRepo, serviceRepo *store.ServiceRepo, networkRepo *store.NetworkRepo,
	domainRepo *store.DomainRepo, certRepo *store.CertificateRepo, backupRepo *store.BackupRepo,
	hardwareRepo *store.HardwareRepo, subscriptionRepo *store.SubscriptionRepo, accountRepo *store.AccountRepo,
	rels *store.RelationshipRepo,
) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		fail := func(err error) { http.Error(w, err.Error(), http.StatusInternalServerError) }

		hosts, err := hostRepo.List()
		if err != nil {
			fail(err)
			return
		}
		services, err := serviceRepo.List()
		if err != nil {
			fail(err)
			return
		}
		networks, err := networkRepo.List()
		if err != nil {
			fail(err)
			return
		}
		domains, err := domainRepo.List()
		if err != nil {
			fail(err)
			return
		}
		certs, err := certRepo.List()
		if err != nil {
			fail(err)
			return
		}
		backups, err := backupRepo.List()
		if err != nil {
			fail(err)
			return
		}
		hardware, err := hardwareRepo.List()
		if err != nil {
			fail(err)
			return
		}
		subscriptions, err := subscriptionRepo.List()
		if err != nil {
			fail(err)
			return
		}
		accounts, err := accountRepo.List()
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

		render(w, "dashboard.html", dashboardData{
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
