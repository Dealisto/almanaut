package web

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

// metricsHandler serves aggregate inventory metrics in the Prometheus text
// exposition format. Every value is a gauge computed on each scrape.
func metricsHandler(repos entityRepos, rels *store.RelationshipRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		hosts, err := repos.hosts.List()
		if err != nil {
			serverError(w, req, err)
			return
		}
		services, err := repos.services.List()
		if err != nil {
			serverError(w, req, err)
			return
		}
		networks, err := repos.networks.List()
		if err != nil {
			serverError(w, req, err)
			return
		}
		domains, err := repos.domains.List()
		if err != nil {
			serverError(w, req, err)
			return
		}
		certs, err := repos.certificates.List()
		if err != nil {
			serverError(w, req, err)
			return
		}
		backups, err := repos.backups.List()
		if err != nil {
			serverError(w, req, err)
			return
		}
		hardware, err := repos.hardware.List()
		if err != nil {
			serverError(w, req, err)
			return
		}
		subs, err := repos.subscriptions.List()
		if err != nil {
			serverError(w, req, err)
			return
		}
		accounts, err := repos.accounts.List()
		if err != nil {
			serverError(w, req, err)
			return
		}
		contacts, err := repos.contacts.List()
		if err != nil {
			serverError(w, req, err)
			return
		}
		sites, err := repos.sites.List()
		if err != nil {
			serverError(w, req, err)
			return
		}
		locations, err := repos.locations.List()
		if err != nil {
			serverError(w, req, err)
			return
		}
		racks, err := repos.racks.List()
		if err != nil {
			serverError(w, req, err)
			return
		}
		relList, err := rels.List()
		if err != nil {
			serverError(w, req, err)
			return
		}

		now := time.Now()
		const withinDays = 30

		var b strings.Builder

		// Per-type entity counts under one metric family.
		b.WriteString("# HELP almanaut_entities_total Number of entities by type.\n")
		b.WriteString("# TYPE almanaut_entities_total gauge\n")
		counts := []struct {
			typ string
			n   int
		}{
			{"host", len(hosts)},
			{"service", len(services)},
			{"network", len(networks)},
			{"domain", len(domains)},
			{"certificate", len(certs)},
			{"backup", len(backups)},
			{"hardware", len(hardware)},
			{"subscription", len(subs)},
			{"account", len(accounts)},
			{"contact", len(contacts)},
			{"site", len(sites)},
			{"location", len(locations)},
			{"rack", len(racks)},
		}
		for _, c := range counts {
			fmt.Fprintf(&b, "almanaut_entities_total{type=%q} %d\n", c.typ, c.n)
		}

		gauge := func(name, help string, value int) {
			fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s gauge\n%s %d\n", name, help, name, name, value)
		}
		gauge("almanaut_relationships_total", "Number of relationships.", len(relList))
		gauge("almanaut_certificates_expiring_total",
			"Certificates expiring within 30 days (including expired).",
			len(domain.ExpiringSoon(certs, now, withinDays)))
		gauge("almanaut_hardware_warranty_expiring_total",
			"Hardware warranties expiring within 30 days.",
			len(domain.WarrantyExpiring(hardware, now, withinDays)))
		gauge("almanaut_subscriptions_renewal_due_total",
			"Subscription renewals due within 30 days.",
			len(domain.RenewalsDue(subs, now, withinDays)))
		gauge("almanaut_hosts_down_total",
			"Hosts whose status marks them down/offline/stopped.",
			len(domain.HostsDown(hosts)))
		gauge("almanaut_services_without_backup_total",
			"Services with no backup relationship.",
			len(domain.ServicesWithoutBackup(services, relList)))

		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = w.Write([]byte(b.String()))
	}
}
