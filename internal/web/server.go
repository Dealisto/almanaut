// Package web wires HTTP routes and renders the server-side UI.
package web

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
	"github.com/go-chi/chi/v5"
)

type servicesPageData struct {
	Title    string
	Services []domain.Service
}

type serviceFormData struct {
	Title, Heading, Action, SubmitLabel, Error string
	Service                                    domain.Service
	Kinds                                      []string
}

type networksPageData struct {
	Title    string
	Networks []domain.Network
}

type networkFormData struct {
	Title, Heading, Action, SubmitLabel, Error string
	Network                                    domain.Network
}

type domainsPageData struct {
	Title   string
	Domains []domain.Domain
}

type domainFormData struct {
	Title, Heading, Action, SubmitLabel, Error string
	Domain                                     domain.Domain
}

type certificatesPageData struct {
	Title        string
	Certificates []domain.Certificate
}

type certificateFormData struct {
	Title, Heading, Action, SubmitLabel, Error string
	Certificate                                domain.Certificate
}

type backupsPageData struct {
	Title   string
	Backups []domain.Backup
}

type backupFormData struct {
	Title, Heading, Action, SubmitLabel, Error string
	Backup                                     domain.Backup
}

type hardwarePageData struct {
	Title    string
	Hardware []domain.Hardware
}

type hardwareFormData struct {
	Title, Heading, Action, SubmitLabel, Error string
	Hardware                                   domain.Hardware
}

type subscriptionsPageData struct {
	Title         string
	Subscriptions []domain.Subscription
}

type subscriptionFormData struct {
	Title, Heading, Action, SubmitLabel, Error string
	Subscription                               domain.Subscription
}

type accountsPageData struct {
	Title    string
	Accounts []domain.Account
}

type accountFormData struct {
	Title, Heading, Action, SubmitLabel, Error string
	Account                                    domain.Account
}

// Config bundles everything New needs to build the HTTP handler. Using a struct
// instead of positional parameters prevents transposing same-typed repos.
type Config struct {
	Hosts         *store.HostRepo
	Services      *store.ServiceRepo
	Networks      *store.NetworkRepo
	Domains       *store.DomainRepo
	Certificates  *store.CertificateRepo
	Backups       *store.BackupRepo
	Hardware      *store.HardwareRepo
	Subscriptions *store.SubscriptionRepo
	Accounts      *store.AccountRepo
	Relationships *store.RelationshipRepo
	Tags          *store.TagRepo
	DB            *sql.DB
	Docker        dockerScanner
	NetScan       networkScanner
	NetOpts       NetDiscoveryOptions
	Proxmox       proxmoxScanner
	PVEOpts       ProxmoxOptions
}

// New builds the HTTP handler with all routes wired to the given repos.
func New(cfg Config) http.Handler {
	hosts, services, networks := cfg.Hosts, cfg.Services, cfg.Networks
	domains, certificates, backups := cfg.Domains, cfg.Certificates, cfg.Backups
	hardware, subscriptions, accounts := cfg.Hardware, cfg.Subscriptions, cfg.Accounts
	relationships, tags, db := cfg.Relationships, cfg.Tags, cfg.DB
	docker, netscan, netOpts := cfg.Docker, cfg.NetScan, cfg.NetOpts
	proxmox, pveOpts := cfg.Proxmox, cfg.PVEOpts
	cat := entityCatalog{
		hosts: hosts, services: services, networks: networks,
		domains: domains, certificates: certificates, backups: backups,
		hardware: hardware, subscriptions: subscriptions, accounts: accounts,
	}
	deps := handlerDeps{cat: cat, tags: tags, rels: relationships}
	resources := []mountable{
		resource[domain.Host]{
			name: "hosts", sing: "host", title: "Hosts", heading: "Host",
			repo:  hosts,
			parse: parseHost,
			label: func(h domain.Host) string { return h.Name },
			id:    func(h domain.Host) int64 { return h.ID },
			notes: func(h domain.Host) string { return h.Notes },
			fields: func(h domain.Host) []fieldRow {
				return []fieldRow{
					{"Type", h.Type}, {"OS", h.OS}, {"CPU", h.CPU}, {"RAM", h.RAM},
					{"Disk", h.Disk}, {"Status", h.Status}, {"IPs", strings.Join(h.IPs, ", ")},
				}
			},
			newItem:  domain.Host{Type: "physical"},
			listTmpl: "hosts.html", formTmpl: "host_form.html",
			extras: func() map[string]any { return map[string]any{"Types": domain.HostTypes} },
		},
	}
	r := chi.NewRouter()
	r.Get("/", dashboard(cat, relationships))
	for _, rs := range resources {
		rs.mount(r, deps)
	}
	r.Post("/tags", addTag(tags))
	r.Post("/tags/delete", removeTag(tags))
	r.Get("/tags", tagsOverview(tags, cat))

	r.Get("/services", listServices(services))
	r.Get("/services/new", newServiceForm())
	r.Post("/services", createService(services))
	r.Get("/services/{id}", showService(services, cat, tags, relationships))
	r.Get("/services/{id}/edit", editServiceForm(services))
	r.Post("/services/{id}", updateService(services))
	r.Post("/services/{id}/delete", deleteService(services, relationships, tags))

	r.Get("/networks", listNetworks(networks))
	r.Get("/networks/new", newNetworkForm())
	r.Post("/networks", createNetwork(networks))
	r.Get("/networks/{id}", showNetwork(networks, cat, tags, relationships))
	r.Get("/networks/{id}/edit", editNetworkForm(networks))
	r.Post("/networks/{id}", updateNetwork(networks))
	r.Post("/networks/{id}/delete", deleteNetwork(networks, relationships, tags))

	r.Get("/domains", listDomains(domains))
	r.Get("/domains/new", newDomainForm())
	r.Post("/domains", createDomain(domains))
	r.Get("/domains/{id}", showDomain(domains, cat, tags, relationships))
	r.Get("/domains/{id}/edit", editDomainForm(domains))
	r.Post("/domains/{id}", updateDomain(domains))
	r.Post("/domains/{id}/delete", deleteDomain(domains, relationships, tags))

	r.Get("/certificates", listCertificates(certificates))
	r.Get("/certificates/new", newCertificateForm())
	r.Post("/certificates", createCertificate(certificates))
	r.Get("/certificates/{id}", showCertificate(certificates, cat, tags, relationships))
	r.Get("/certificates/{id}/edit", editCertificateForm(certificates))
	r.Post("/certificates/{id}", updateCertificate(certificates))
	r.Post("/certificates/{id}/delete", deleteCertificate(certificates, relationships, tags))

	r.Get("/backups", listBackups(backups))
	r.Get("/backups/new", newBackupForm())
	r.Post("/backups", createBackup(backups))
	r.Get("/backups/{id}", showBackup(backups, cat, tags, relationships))
	r.Get("/backups/{id}/edit", editBackupForm(backups))
	r.Post("/backups/{id}", updateBackup(backups))
	r.Post("/backups/{id}/delete", deleteBackup(backups, relationships, tags))

	r.Get("/hardware", listHardware(hardware))
	r.Get("/hardware/new", newHardwareForm())
	r.Post("/hardware", createHardware(hardware))
	r.Get("/hardware/{id}", showHardware(hardware, cat, tags, relationships))
	r.Get("/hardware/{id}/edit", editHardwareForm(hardware))
	r.Post("/hardware/{id}", updateHardware(hardware))
	r.Post("/hardware/{id}/delete", deleteHardware(hardware, relationships, tags))

	r.Get("/subscriptions", listSubscriptions(subscriptions))
	r.Get("/subscriptions/new", newSubscriptionForm())
	r.Post("/subscriptions", createSubscription(subscriptions))
	r.Get("/subscriptions/{id}", showSubscription(subscriptions, cat, tags, relationships))
	r.Get("/subscriptions/{id}/edit", editSubscriptionForm(subscriptions))
	r.Post("/subscriptions/{id}", updateSubscription(subscriptions))
	r.Post("/subscriptions/{id}/delete", deleteSubscription(subscriptions, relationships, tags))

	r.Get("/accounts", listAccounts(accounts))
	r.Get("/accounts/new", newAccountForm())
	r.Post("/accounts", createAccount(accounts))
	r.Get("/accounts/{id}", showAccount(accounts, cat, tags, relationships))
	r.Get("/accounts/{id}/edit", editAccountForm(accounts))
	r.Post("/accounts/{id}", updateAccount(accounts))
	r.Post("/accounts/{id}/delete", deleteAccount(accounts, relationships, tags))

	r.Get("/relationships", listRelationships(relationships, cat))
	r.Post("/relationships", createRelationship(relationships, cat))
	r.Post("/relationships/{id}/delete", deleteRelationship(relationships))
	r.Get("/impact", impactView(relationships, cat))
	r.Get("/checks", healthChecks(services, certificates, hardware, subscriptions, relationships))
	r.Get("/search", searchEntities(cat, tags))
	r.Get("/data", showData())
	r.Get("/export", exportData(db))
	r.Post("/import", importData(db))
	r.Get("/discovery", discoveryLanding(netOpts, pveOpts))
	r.Get("/discovery/docker", scanDocker(docker, services, hosts))
	r.Post("/discovery/docker/import", importDocker(docker, services, relationships, db))
	r.Get("/discovery/network", networkForm(netOpts))
	r.Post("/discovery/network/scan", scanNetwork(netscan, hosts, netOpts))
	r.Post("/discovery/network/import", importNetwork(hosts, netOpts, db))
	r.Get("/discovery/proxmox", scanProxmox(proxmox, hosts, pveOpts))
	r.Post("/discovery/proxmox/import", importProxmox(proxmox, hosts, relationships, pveOpts, db))
	return r
}

type impactPageData struct {
	Title         string
	Options       []entityOption
	Selected      string
	SelectedLabel string
	Impacted      []string
	Computed      bool
	Error         string
}

func impactView(rels *store.RelationshipRepo, cat entityCatalog) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		opts, err := cat.options()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		data := impactPageData{Title: "Impact", Options: opts}

		ref := req.URL.Query().Get("ref")
		if ref != "" {
			typ, id, perr := parseRef(ref)
			if perr != nil {
				data.Error = perr.Error()
				render(w, "impact.html", data)
				return
			}
			labels := labelMap(opts)
			data.Selected = ref
			data.SelectedLabel = labelOrFallback(labels, typ, id)
			refs, err := store.Impact(rels, typ, id)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			data.Computed = true
			for _, r := range refs {
				data.Impacted = append(data.Impacted, labelOrFallback(labels, r.Type, r.ID))
			}
		}
		render(w, "impact.html", data)
	}
}

type checksPageData struct {
	Title              string
	WithinDays         int
	UnbackedServices   []domain.Service
	ExpiringCerts      []domain.Certificate
	ExpiringWarranties []domain.Hardware
	RenewalsDue        []domain.Subscription
}

func healthChecks(services *store.ServiceRepo, certs *store.CertificateRepo, hardware *store.HardwareRepo, subscriptions *store.SubscriptionRepo, rels *store.RelationshipRepo) http.HandlerFunc {
	const withinDays = 30
	return func(w http.ResponseWriter, req *http.Request) {
		svcList, err := services.List()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		certList, err := certs.List()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		hwList, err := hardware.List()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		subList, err := subscriptions.List()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		relList, err := rels.List()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		render(w, "checks.html", checksPageData{
			Title:              "Checks",
			WithinDays:         withinDays,
			UnbackedServices:   domain.ServicesWithoutBackup(svcList, relList),
			ExpiringCerts:      domain.ExpiringSoon(certList, time.Now(), withinDays),
			ExpiringWarranties: domain.WarrantyExpiring(hwList, time.Now(), withinDays),
			RenewalsDue:        domain.RenewalsDue(subList, time.Now(), withinDays),
		})
	}
}

type relationshipView struct {
	ID                       int64
	FromLabel, Kind, ToLabel string
}

type relationshipsPageData struct {
	Title         string
	Relationships []relationshipView
	Options       []entityOption
	Kinds         []string
	Error         string
}

func renderRelationships(w http.ResponseWriter, rels *store.RelationshipRepo, cat entityCatalog, errMsg string) {
	opts, err := cat.options()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	labels := labelMap(opts)
	all, err := rels.List()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	views := make([]relationshipView, 0, len(all))
	for _, rel := range all {
		views = append(views, relationshipView{
			ID:        rel.ID,
			FromLabel: labelOrFallback(labels, rel.FromType, rel.FromID),
			Kind:      rel.Kind,
			ToLabel:   labelOrFallback(labels, rel.ToType, rel.ToID),
		})
	}
	render(w, "relationships.html", relationshipsPageData{
		Title: "Relationships", Relationships: views, Options: opts,
		Kinds: domain.RelationshipKinds, Error: errMsg,
	})
}

func listRelationships(rels *store.RelationshipRepo, cat entityCatalog) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		renderRelationships(w, rels, cat, "")
	}
}

func createRelationship(rels *store.RelationshipRepo, cat entityCatalog) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		fromType, fromID, errFrom := parseRef(req.FormValue("from"))
		toType, toID, errTo := parseRef(req.FormValue("to"))
		rel := domain.Relationship{
			FromType: fromType, FromID: fromID,
			ToType: toType, ToID: toID, Kind: req.FormValue("kind"),
		}
		if errFrom != nil {
			renderRelationships(w, rels, cat, errFrom.Error())
			return
		}
		if errTo != nil {
			renderRelationships(w, rels, cat, errTo.Error())
			return
		}
		if err := rel.Validate(); err != nil {
			renderRelationships(w, rels, cat, err.Error())
			return
		}
		if _, err := rels.Create(rel); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, req, "/relationships", http.StatusSeeOther)
	}
}

func deleteRelationship(rels *store.RelationshipRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		if err := rels.Delete(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, req, "/relationships", http.StatusSeeOther)
	}
}

func listServices(repo *store.ServiceRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		services, err := repo.List()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		render(w, "services.html", servicesPageData{Title: "Services", Services: services})
	}
}

func newServiceForm() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		render(w, "service_form.html", serviceFormData{
			Title: "New service", Heading: "New service", Action: "/services",
			SubmitLabel: "Create", Service: domain.Service{Kind: "container"}, Kinds: domain.ServiceKinds,
		})
	}
}

func serviceFromForm(req *http.Request) domain.Service {
	return domain.Service{
		Name:     strings.TrimSpace(req.FormValue("name")),
		Kind:     req.FormValue("kind"),
		URL:      req.FormValue("url"),
		Ports:    req.FormValue("ports"),
		Category: req.FormValue("category"),
		Notes:    req.FormValue("notes"),
	}
}

func createService(repo *store.ServiceRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		svc := serviceFromForm(req)
		if err := svc.Validate(); err != nil {
			render(w, "service_form.html", serviceFormData{
				Title: "New service", Heading: "New service", Action: "/services",
				SubmitLabel: "Create", Service: svc, Kinds: domain.ServiceKinds, Error: err.Error(),
			})
			return
		}
		if _, err := repo.Create(svc); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, req, "/services", http.StatusSeeOther)
	}
}

func editServiceForm(repo *store.ServiceRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		svc, err := repo.Get(id)
		if err != nil {
			http.Error(w, "service not found", http.StatusNotFound)
			return
		}
		render(w, "service_form.html", serviceFormData{
			Title: "Edit service", Heading: "Edit service", Action: fmt.Sprintf("/services/%d", id),
			SubmitLabel: "Save", Service: svc, Kinds: domain.ServiceKinds,
		})
	}
}

func updateService(repo *store.ServiceRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		svc := serviceFromForm(req)
		svc.ID = id
		if err := svc.Validate(); err != nil {
			render(w, "service_form.html", serviceFormData{
				Title: "Edit service", Heading: "Edit service", Action: fmt.Sprintf("/services/%d", id),
				SubmitLabel: "Save", Service: svc, Kinds: domain.ServiceKinds, Error: err.Error(),
			})
			return
		}
		if err := repo.Update(svc); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, req, "/services", http.StatusSeeOther)
	}
}

func deleteService(repo *store.ServiceRepo, rels *store.RelationshipRepo, tags *store.TagRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		if err := repo.Delete(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := rels.DeleteByEntity("service", id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := tags.DeleteByEntity("service", id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, req, "/services", http.StatusSeeOther)
	}
}

func listNetworks(repo *store.NetworkRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		networks, err := repo.List()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		render(w, "networks.html", networksPageData{Title: "Networks", Networks: networks})
	}
}

func newNetworkForm() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		render(w, "network_form.html", networkFormData{
			Title: "New network", Heading: "New network", Action: "/networks", SubmitLabel: "Create",
		})
	}
}

func networkFromForm(req *http.Request) domain.Network {
	return domain.Network{
		Name:    strings.TrimSpace(req.FormValue("name")),
		CIDR:    strings.TrimSpace(req.FormValue("cidr")),
		VLAN:    req.FormValue("vlan"),
		Gateway: strings.TrimSpace(req.FormValue("gateway")),
		Notes:   req.FormValue("notes"),
	}
}

func createNetwork(repo *store.NetworkRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		n := networkFromForm(req)
		if err := n.Validate(); err != nil {
			render(w, "network_form.html", networkFormData{
				Title: "New network", Heading: "New network", Action: "/networks",
				SubmitLabel: "Create", Network: n, Error: err.Error(),
			})
			return
		}
		if _, err := repo.Create(n); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, req, "/networks", http.StatusSeeOther)
	}
}

func editNetworkForm(repo *store.NetworkRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		n, err := repo.Get(id)
		if err != nil {
			http.Error(w, "network not found", http.StatusNotFound)
			return
		}
		render(w, "network_form.html", networkFormData{
			Title: "Edit network", Heading: "Edit network", Action: fmt.Sprintf("/networks/%d", id),
			SubmitLabel: "Save", Network: n,
		})
	}
}

func updateNetwork(repo *store.NetworkRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		n := networkFromForm(req)
		n.ID = id
		if err := n.Validate(); err != nil {
			render(w, "network_form.html", networkFormData{
				Title: "Edit network", Heading: "Edit network", Action: fmt.Sprintf("/networks/%d", id),
				SubmitLabel: "Save", Network: n, Error: err.Error(),
			})
			return
		}
		if err := repo.Update(n); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, req, "/networks", http.StatusSeeOther)
	}
}

func deleteNetwork(repo *store.NetworkRepo, rels *store.RelationshipRepo, tags *store.TagRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		if err := repo.Delete(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := rels.DeleteByEntity("network", id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := tags.DeleteByEntity("network", id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, req, "/networks", http.StatusSeeOther)
	}
}

func listDomains(repo *store.DomainRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		domains, err := repo.List()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		render(w, "domains.html", domainsPageData{Title: "Domains", Domains: domains})
	}
}

func newDomainForm() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		render(w, "domain_form.html", domainFormData{
			Title: "New domain", Heading: "New domain", Action: "/domains", SubmitLabel: "Create",
		})
	}
}

func domainFromForm(req *http.Request) domain.Domain {
	return domain.Domain{
		FQDN:     strings.TrimSpace(req.FormValue("fqdn")),
		Provider: strings.TrimSpace(req.FormValue("provider")),
		Notes:    req.FormValue("notes"),
	}
}

func createDomain(repo *store.DomainRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		d := domainFromForm(req)
		if err := d.Validate(); err != nil {
			render(w, "domain_form.html", domainFormData{
				Title: "New domain", Heading: "New domain", Action: "/domains",
				SubmitLabel: "Create", Domain: d, Error: err.Error(),
			})
			return
		}
		if _, err := repo.Create(d); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, req, "/domains", http.StatusSeeOther)
	}
}

func editDomainForm(repo *store.DomainRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		d, err := repo.Get(id)
		if err != nil {
			http.Error(w, "domain not found", http.StatusNotFound)
			return
		}
		render(w, "domain_form.html", domainFormData{
			Title: "Edit domain", Heading: "Edit domain", Action: fmt.Sprintf("/domains/%d", id),
			SubmitLabel: "Save", Domain: d,
		})
	}
}

func updateDomain(repo *store.DomainRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		d := domainFromForm(req)
		d.ID = id
		if err := d.Validate(); err != nil {
			render(w, "domain_form.html", domainFormData{
				Title: "Edit domain", Heading: "Edit domain", Action: fmt.Sprintf("/domains/%d", id),
				SubmitLabel: "Save", Domain: d, Error: err.Error(),
			})
			return
		}
		if err := repo.Update(d); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, req, "/domains", http.StatusSeeOther)
	}
}

func deleteDomain(repo *store.DomainRepo, rels *store.RelationshipRepo, tags *store.TagRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		if err := repo.Delete(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := rels.DeleteByEntity("domain", id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := tags.DeleteByEntity("domain", id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, req, "/domains", http.StatusSeeOther)
	}
}

func listCertificates(repo *store.CertificateRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		certs, err := repo.List()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		render(w, "certificates.html", certificatesPageData{Title: "Certificates", Certificates: certs})
	}
}

func newCertificateForm() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		render(w, "certificate_form.html", certificateFormData{
			Title: "New certificate", Heading: "New certificate", Action: "/certificates", SubmitLabel: "Create",
		})
	}
}

func certificateFromForm(req *http.Request) domain.Certificate {
	return domain.Certificate{
		Subject:   strings.TrimSpace(req.FormValue("subject")),
		Issuer:    strings.TrimSpace(req.FormValue("issuer")),
		ExpiresOn: strings.TrimSpace(req.FormValue("expires_on")),
		AutoRenew: req.FormValue("auto_renew") == "on",
		Notes:     req.FormValue("notes"),
	}
}

func createCertificate(repo *store.CertificateRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		c := certificateFromForm(req)
		if err := c.Validate(); err != nil {
			render(w, "certificate_form.html", certificateFormData{
				Title: "New certificate", Heading: "New certificate", Action: "/certificates",
				SubmitLabel: "Create", Certificate: c, Error: err.Error(),
			})
			return
		}
		if _, err := repo.Create(c); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, req, "/certificates", http.StatusSeeOther)
	}
}

func editCertificateForm(repo *store.CertificateRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		c, err := repo.Get(id)
		if err != nil {
			http.Error(w, "certificate not found", http.StatusNotFound)
			return
		}
		render(w, "certificate_form.html", certificateFormData{
			Title: "Edit certificate", Heading: "Edit certificate", Action: fmt.Sprintf("/certificates/%d", id),
			SubmitLabel: "Save", Certificate: c,
		})
	}
}

func updateCertificate(repo *store.CertificateRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		c := certificateFromForm(req)
		c.ID = id
		if err := c.Validate(); err != nil {
			render(w, "certificate_form.html", certificateFormData{
				Title: "Edit certificate", Heading: "Edit certificate", Action: fmt.Sprintf("/certificates/%d", id),
				SubmitLabel: "Save", Certificate: c, Error: err.Error(),
			})
			return
		}
		if err := repo.Update(c); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, req, "/certificates", http.StatusSeeOther)
	}
}

func deleteCertificate(repo *store.CertificateRepo, rels *store.RelationshipRepo, tags *store.TagRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		if err := repo.Delete(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := rels.DeleteByEntity("certificate", id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := tags.DeleteByEntity("certificate", id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, req, "/certificates", http.StatusSeeOther)
	}
}

func listBackups(repo *store.BackupRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		backups, err := repo.List()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		render(w, "backups.html", backupsPageData{Title: "Backups", Backups: backups})
	}
}

func newBackupForm() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		render(w, "backup_form.html", backupFormData{
			Title: "New backup", Heading: "New backup", Action: "/backups", SubmitLabel: "Create",
		})
	}
}

func backupFromForm(req *http.Request) domain.Backup {
	return domain.Backup{
		Source:      strings.TrimSpace(req.FormValue("source")),
		Destination: strings.TrimSpace(req.FormValue("destination")),
		Frequency:   strings.TrimSpace(req.FormValue("frequency")),
		LastRun:     strings.TrimSpace(req.FormValue("last_run")),
		Notes:       req.FormValue("notes"),
	}
}

func createBackup(repo *store.BackupRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		b := backupFromForm(req)
		if err := b.Validate(); err != nil {
			render(w, "backup_form.html", backupFormData{
				Title: "New backup", Heading: "New backup", Action: "/backups",
				SubmitLabel: "Create", Backup: b, Error: err.Error(),
			})
			return
		}
		if _, err := repo.Create(b); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, req, "/backups", http.StatusSeeOther)
	}
}

func editBackupForm(repo *store.BackupRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		b, err := repo.Get(id)
		if err != nil {
			http.Error(w, "backup not found", http.StatusNotFound)
			return
		}
		render(w, "backup_form.html", backupFormData{
			Title: "Edit backup", Heading: "Edit backup", Action: fmt.Sprintf("/backups/%d", id),
			SubmitLabel: "Save", Backup: b,
		})
	}
}

func updateBackup(repo *store.BackupRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		b := backupFromForm(req)
		b.ID = id
		if err := b.Validate(); err != nil {
			render(w, "backup_form.html", backupFormData{
				Title: "Edit backup", Heading: "Edit backup", Action: fmt.Sprintf("/backups/%d", id),
				SubmitLabel: "Save", Backup: b, Error: err.Error(),
			})
			return
		}
		if err := repo.Update(b); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, req, "/backups", http.StatusSeeOther)
	}
}

func deleteBackup(repo *store.BackupRepo, rels *store.RelationshipRepo, tags *store.TagRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		if err := repo.Delete(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := rels.DeleteByEntity("backup", id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := tags.DeleteByEntity("backup", id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, req, "/backups", http.StatusSeeOther)
	}
}

func showBackup(repo *store.BackupRepo, cat entityCatalog, tags *store.TagRepo, rels *store.RelationshipRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		b, err := repo.Get(id)
		if err != nil {
			http.Error(w, "backup not found", http.StatusNotFound)
			return
		}
		fields := []fieldRow{
			{"Destination", b.Destination},
			{"Frequency", b.Frequency},
			{"Last run", b.LastRun},
		}
		renderDetail(w, cat, tags, rels, "backup", id,
			"Backup: "+b.Source, b.Notes, fmt.Sprintf("/backups/%d/edit", id), fields)
	}
}

func listHardware(repo *store.HardwareRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		items, err := repo.List()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		render(w, "hardware.html", hardwarePageData{Title: "Hardware", Hardware: items})
	}
}

func newHardwareForm() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		render(w, "hardware_form.html", hardwareFormData{
			Title: "New hardware", Heading: "New hardware", Action: "/hardware", SubmitLabel: "Create",
		})
	}
}

func hardwareFromForm(req *http.Request) domain.Hardware {
	return domain.Hardware{
		Name:         strings.TrimSpace(req.FormValue("name")),
		Kind:         strings.TrimSpace(req.FormValue("kind")),
		Manufacturer: strings.TrimSpace(req.FormValue("manufacturer")),
		Model:        strings.TrimSpace(req.FormValue("model")),
		Serial:       strings.TrimSpace(req.FormValue("serial")),
		Location:     strings.TrimSpace(req.FormValue("location")),
		PurchaseDate: strings.TrimSpace(req.FormValue("purchase_date")),
		WarrantyEnd:  strings.TrimSpace(req.FormValue("warranty_end")),
		Status:       strings.TrimSpace(req.FormValue("status")),
		Notes:        req.FormValue("notes"),
	}
}

func createHardware(repo *store.HardwareRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		h := hardwareFromForm(req)
		if err := h.Validate(); err != nil {
			render(w, "hardware_form.html", hardwareFormData{
				Title: "New hardware", Heading: "New hardware", Action: "/hardware",
				SubmitLabel: "Create", Hardware: h, Error: err.Error(),
			})
			return
		}
		if _, err := repo.Create(h); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, req, "/hardware", http.StatusSeeOther)
	}
}

func editHardwareForm(repo *store.HardwareRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		h, err := repo.Get(id)
		if err != nil {
			http.Error(w, "hardware not found", http.StatusNotFound)
			return
		}
		render(w, "hardware_form.html", hardwareFormData{
			Title: "Edit hardware", Heading: "Edit hardware", Action: fmt.Sprintf("/hardware/%d", id),
			SubmitLabel: "Save", Hardware: h,
		})
	}
}

func updateHardware(repo *store.HardwareRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		h := hardwareFromForm(req)
		h.ID = id
		if err := h.Validate(); err != nil {
			render(w, "hardware_form.html", hardwareFormData{
				Title: "Edit hardware", Heading: "Edit hardware", Action: fmt.Sprintf("/hardware/%d", id),
				SubmitLabel: "Save", Hardware: h, Error: err.Error(),
			})
			return
		}
		if err := repo.Update(h); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, req, "/hardware", http.StatusSeeOther)
	}
}

func deleteHardware(repo *store.HardwareRepo, rels *store.RelationshipRepo, tags *store.TagRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		if err := repo.Delete(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := rels.DeleteByEntity("hardware", id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := tags.DeleteByEntity("hardware", id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, req, "/hardware", http.StatusSeeOther)
	}
}

func showHardware(repo *store.HardwareRepo, cat entityCatalog, tags *store.TagRepo, rels *store.RelationshipRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		h, err := repo.Get(id)
		if err != nil {
			http.Error(w, "hardware not found", http.StatusNotFound)
			return
		}
		fields := []fieldRow{
			{"Kind", h.Kind},
			{"Manufacturer", h.Manufacturer},
			{"Model", h.Model},
			{"Serial", h.Serial},
			{"Location", h.Location},
			{"Purchase date", h.PurchaseDate},
			{"Warranty end", h.WarrantyEnd},
			{"Status", h.Status},
		}
		renderDetail(w, cat, tags, rels, "hardware", id,
			"Hardware: "+h.Name, h.Notes, fmt.Sprintf("/hardware/%d/edit", id), fields)
	}
}

func listSubscriptions(repo *store.SubscriptionRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		subs, err := repo.List()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		render(w, "subscriptions.html", subscriptionsPageData{Title: "Subscriptions", Subscriptions: subs})
	}
}

func newSubscriptionForm() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		render(w, "subscription_form.html", subscriptionFormData{
			Title: "New subscription", Heading: "New subscription", Action: "/subscriptions", SubmitLabel: "Create",
		})
	}
}

func subscriptionFromForm(req *http.Request) domain.Subscription {
	return domain.Subscription{
		Name:         strings.TrimSpace(req.FormValue("name")),
		Kind:         strings.TrimSpace(req.FormValue("kind")),
		Provider:     strings.TrimSpace(req.FormValue("provider")),
		Amount:       strings.TrimSpace(req.FormValue("amount")),
		Currency:     strings.TrimSpace(req.FormValue("currency")),
		BillingCycle: strings.TrimSpace(req.FormValue("billing_cycle")),
		RenewalDate:  strings.TrimSpace(req.FormValue("renewal_date")),
		AutoRenew:    req.FormValue("auto_renew") == "on",
		Status:       strings.TrimSpace(req.FormValue("status")),
		Notes:        req.FormValue("notes"),
	}
}

func createSubscription(repo *store.SubscriptionRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		s := subscriptionFromForm(req)
		if err := s.Validate(); err != nil {
			render(w, "subscription_form.html", subscriptionFormData{
				Title: "New subscription", Heading: "New subscription", Action: "/subscriptions",
				SubmitLabel: "Create", Subscription: s, Error: err.Error(),
			})
			return
		}
		if _, err := repo.Create(s); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, req, "/subscriptions", http.StatusSeeOther)
	}
}

func editSubscriptionForm(repo *store.SubscriptionRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		s, err := repo.Get(id)
		if err != nil {
			http.Error(w, "subscription not found", http.StatusNotFound)
			return
		}
		render(w, "subscription_form.html", subscriptionFormData{
			Title: "Edit subscription", Heading: "Edit subscription", Action: fmt.Sprintf("/subscriptions/%d", id),
			SubmitLabel: "Save", Subscription: s,
		})
	}
}

func updateSubscription(repo *store.SubscriptionRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		s := subscriptionFromForm(req)
		s.ID = id
		if err := s.Validate(); err != nil {
			render(w, "subscription_form.html", subscriptionFormData{
				Title: "Edit subscription", Heading: "Edit subscription", Action: fmt.Sprintf("/subscriptions/%d", id),
				SubmitLabel: "Save", Subscription: s, Error: err.Error(),
			})
			return
		}
		if err := repo.Update(s); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, req, "/subscriptions", http.StatusSeeOther)
	}
}

func deleteSubscription(repo *store.SubscriptionRepo, rels *store.RelationshipRepo, tags *store.TagRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		if err := repo.Delete(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := rels.DeleteByEntity("subscription", id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := tags.DeleteByEntity("subscription", id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, req, "/subscriptions", http.StatusSeeOther)
	}
}

func showSubscription(repo *store.SubscriptionRepo, cat entityCatalog, tags *store.TagRepo, rels *store.RelationshipRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		s, err := repo.Get(id)
		if err != nil {
			http.Error(w, "subscription not found", http.StatusNotFound)
			return
		}
		price := s.Amount
		if s.Amount != "" && s.Currency != "" {
			price = s.Amount + " " + s.Currency
		}
		autoRenew := "no"
		if s.AutoRenew {
			autoRenew = "yes"
		}
		fields := []fieldRow{
			{"Kind", s.Kind},
			{"Provider", s.Provider},
			{"Amount", price},
			{"Billing cycle", s.BillingCycle},
			{"Renewal date", s.RenewalDate},
			{"Auto-renew", autoRenew},
			{"Status", s.Status},
		}
		renderDetail(w, cat, tags, rels, "subscription", id,
			"Subscription: "+s.Name, s.Notes, fmt.Sprintf("/subscriptions/%d/edit", id), fields)
	}
}

func showService(repo *store.ServiceRepo, cat entityCatalog, tags *store.TagRepo, rels *store.RelationshipRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		s, err := repo.Get(id)
		if err != nil {
			http.Error(w, "service not found", http.StatusNotFound)
			return
		}
		fields := []fieldRow{
			{"Kind", s.Kind},
			{"URL", s.URL},
			{"Ports", s.Ports},
			{"Category", s.Category},
		}
		renderDetail(w, cat, tags, rels, "service", id,
			"Service: "+s.Name, s.Notes, fmt.Sprintf("/services/%d/edit", id), fields)
	}
}

func showNetwork(repo *store.NetworkRepo, cat entityCatalog, tags *store.TagRepo, rels *store.RelationshipRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		n, err := repo.Get(id)
		if err != nil {
			http.Error(w, "network not found", http.StatusNotFound)
			return
		}
		nets, err := repo.List()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		hosts, err := cat.hosts.List()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		var section *ipamSection
		for _, u := range domain.BuildIPAM(nets, hosts).Networks {
			if u.Network.ID == id {
				s := buildIPAMSection(u)
				section = &s
				break
			}
		}
		fields := []fieldRow{
			{"CIDR", n.CIDR},
			{"VLAN", n.VLAN},
			{"Gateway", n.Gateway},
		}
		renderDetailExtra(w, cat, tags, rels, "network", id,
			"Network: "+n.Name, n.Notes, fmt.Sprintf("/networks/%d/edit", id), fields, section)
	}
}

func showDomain(repo *store.DomainRepo, cat entityCatalog, tags *store.TagRepo, rels *store.RelationshipRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		d, err := repo.Get(id)
		if err != nil {
			http.Error(w, "domain not found", http.StatusNotFound)
			return
		}
		fields := []fieldRow{
			{"Provider", d.Provider},
		}
		renderDetail(w, cat, tags, rels, "domain", id,
			"Domain: "+d.FQDN, d.Notes, fmt.Sprintf("/domains/%d/edit", id), fields)
	}
}

func showCertificate(repo *store.CertificateRepo, cat entityCatalog, tags *store.TagRepo, rels *store.RelationshipRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		c, err := repo.Get(id)
		if err != nil {
			http.Error(w, "certificate not found", http.StatusNotFound)
			return
		}
		autoRenew := "no"
		if c.AutoRenew {
			autoRenew = "yes"
		}
		fields := []fieldRow{
			{"Issuer", c.Issuer},
			{"Expires on", c.ExpiresOn},
			{"Auto-renew", autoRenew},
		}
		renderDetail(w, cat, tags, rels, "certificate", id,
			"Certificate: "+c.Subject, c.Notes, fmt.Sprintf("/certificates/%d/edit", id), fields)
	}
}

type tagEntity struct {
	Label string
	URL   string
}

type tagsOverviewData struct {
	Title    string
	Counts   []store.TagCount
	Selected string
	Entities []tagEntity
}

func tagsOverview(tags *store.TagRepo, cat entityCatalog) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		// Decide between the tag cloud and a drilldown on whether a name was
		// actually requested, not on its normalized form — otherwise a query
		// like ?name=# (which normalizes to "") would silently show the cloud.
		raw := strings.TrimSpace(req.URL.Query().Get("name"))
		if raw == "" {
			counts, err := tags.Counts()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			render(w, "tags_overview.html", tagsOverviewData{Title: "Tags", Counts: counts})
			return
		}

		name := domain.NormalizeTag(raw)
		tagged, err := tags.ListByName(name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		opts, err := cat.options()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		labels := labelMap(opts)
		entities := make([]tagEntity, 0, len(tagged))
		for _, tg := range tagged {
			entities = append(entities, tagEntity{
				Label: labelOrFallback(labels, tg.EntityType, tg.EntityID),
				URL:   fmt.Sprintf("/%ss/%d", tg.EntityType, tg.EntityID),
			})
		}
		// Keep the view in drilldown mode (Selected non-empty) even when the
		// requested name normalizes away, so it shows "no entities" not the cloud.
		selected := name
		if selected == "" {
			selected = raw
		}
		render(w, "tags_overview.html", tagsOverviewData{
			Title: "Tags", Selected: selected, Entities: entities,
		})
	}
}

func addTag(tags *store.TagRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		entityType := req.FormValue("entity_type")
		entityID, err := strconv.ParseInt(req.FormValue("entity_id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid entity id", http.StatusBadRequest)
			return
		}
		tag := domain.Tag{EntityType: entityType, EntityID: entityID, Name: req.FormValue("tag")}
		if err := tag.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := tags.Add(tag); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, req, fmt.Sprintf("/%ss/%d", entityType, entityID), http.StatusSeeOther)
	}
}

func removeTag(tags *store.TagRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(req.FormValue("id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		if err := tags.Delete(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		entityType := req.FormValue("entity_type")
		entityID, _ := strconv.ParseInt(req.FormValue("entity_id"), 10, 64)
		http.Redirect(w, req, fmt.Sprintf("/%ss/%d", entityType, entityID), http.StatusSeeOther)
	}
}

func accountFromForm(req *http.Request) domain.Account {
	return domain.Account{
		Name:            strings.TrimSpace(req.FormValue("name")),
		Kind:            strings.TrimSpace(req.FormValue("kind")),
		Username:        strings.TrimSpace(req.FormValue("username")),
		PasswordManager: strings.TrimSpace(req.FormValue("password_manager")),
		SecretRef:       strings.TrimSpace(req.FormValue("secret_ref")),
		URL:             strings.TrimSpace(req.FormValue("url")),
		Status:          strings.TrimSpace(req.FormValue("status")),
		Notes:           req.FormValue("notes"),
	}
}

func listAccounts(repo *store.AccountRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		accounts, err := repo.List()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		render(w, "accounts.html", accountsPageData{Title: "Accounts", Accounts: accounts})
	}
}

func newAccountForm() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		render(w, "account_form.html", accountFormData{
			Title: "New account", Heading: "New account",
			Action: "/accounts", SubmitLabel: "Create",
		})
	}
}

func createAccount(repo *store.AccountRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		a := accountFromForm(req)
		if err := a.Validate(); err != nil {
			render(w, "account_form.html", accountFormData{
				Title: "New account", Heading: "New account", Action: "/accounts",
				SubmitLabel: "Create", Account: a, Error: err.Error(),
			})
			return
		}
		if _, err := repo.Create(a); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, req, "/accounts", http.StatusSeeOther)
	}
}

func showAccount(repo *store.AccountRepo, cat entityCatalog, tags *store.TagRepo, rels *store.RelationshipRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		a, err := repo.Get(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		fields := []fieldRow{
			{"Kind", a.Kind},
			{"Username", a.Username},
			{"Password manager", a.PasswordManager},
			{"Secret ref", a.SecretRef},
			{"URL", a.URL},
			{"Status", a.Status},
		}
		renderDetail(w, cat, tags, rels, "account", id,
			"Account: "+a.Name, a.Notes, fmt.Sprintf("/accounts/%d/edit", id), fields)
	}
}

func editAccountForm(repo *store.AccountRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		a, err := repo.Get(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		render(w, "account_form.html", accountFormData{
			Title: "Edit account", Heading: "Edit account",
			Action: fmt.Sprintf("/accounts/%d", id), SubmitLabel: "Save", Account: a,
		})
	}
}

func updateAccount(repo *store.AccountRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		a := accountFromForm(req)
		a.ID = id
		if err := a.Validate(); err != nil {
			render(w, "account_form.html", accountFormData{
				Title: "Edit account", Heading: "Edit account",
				Action: fmt.Sprintf("/accounts/%d", id), SubmitLabel: "Save",
				Account: a, Error: err.Error(),
			})
			return
		}
		if err := repo.Update(a); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, req, "/accounts", http.StatusSeeOther)
	}
}

func deleteAccount(repo *store.AccountRepo, rels *store.RelationshipRepo, tags *store.TagRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		if err := repo.Delete(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := rels.DeleteByEntity("account", id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := tags.DeleteByEntity("account", id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, req, "/accounts", http.StatusSeeOther)
	}
}

// parseIPs splits a comma-separated field into trimmed, non-empty values.
func parseIPs(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}
