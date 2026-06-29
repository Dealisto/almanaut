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
		resource[domain.Service]{
			name: "services", sing: "service", title: "Services", heading: "Service",
			repo:  services,
			parse: parseService,
			label: func(s domain.Service) string { return s.Name },
			id:    func(s domain.Service) int64 { return s.ID },
			notes: func(s domain.Service) string { return s.Notes },
			fields: func(s domain.Service) []fieldRow {
				return []fieldRow{
					{"Kind", s.Kind},
					{"URL", s.URL},
					{"Ports", s.Ports},
					{"Category", s.Category},
				}
			},
			newItem:  domain.Service{Kind: "container"},
			listTmpl: "services.html", formTmpl: "service_form.html",
			extras: func() map[string]any { return map[string]any{"Kinds": domain.ServiceKinds} },
		},
		resource[domain.Network]{
			name: "networks", sing: "network", title: "Networks", heading: "Network",
			repo:  networks,
			parse: parseNetwork,
			label: func(n domain.Network) string { return n.Name },
			id:    func(n domain.Network) int64 { return n.ID },
			notes: func(n domain.Network) string { return n.Notes },
			fields: func(n domain.Network) []fieldRow {
				return []fieldRow{
					{"CIDR", n.CIDR},
					{"VLAN", n.VLAN},
					{"Gateway", n.Gateway},
				}
			},
			newItem:  domain.Network{},
			listTmpl: "networks.html", formTmpl: "network_form.html",
			ipam: func(n domain.Network) *ipamSection {
				nets, err := networks.List()
				if err != nil {
					return nil
				}
				hostList, err := hosts.List()
				if err != nil {
					return nil
				}
				for _, u := range domain.BuildIPAM(nets, hostList).Networks {
					if u.Network.ID == n.ID {
						s := buildIPAMSection(u)
						return &s
					}
				}
				return nil
			},
		},
		resource[domain.Domain]{
			name: "domains", sing: "domain", title: "Domains", heading: "Domain",
			repo:  domains,
			parse: parseDomain,
			label: func(d domain.Domain) string { return d.FQDN },
			id:    func(d domain.Domain) int64 { return d.ID },
			notes: func(d domain.Domain) string { return d.Notes },
			fields: func(d domain.Domain) []fieldRow {
				return []fieldRow{
					{"Provider", d.Provider},
				}
			},
			newItem:  domain.Domain{},
			listTmpl: "domains.html", formTmpl: "domain_form.html",
		},
		resource[domain.Certificate]{
			name: "certificates", sing: "certificate", title: "Certificates", heading: "Certificate",
			repo:  certificates,
			parse: parseCertificate,
			label: func(c domain.Certificate) string { return c.Subject },
			id:    func(c domain.Certificate) int64 { return c.ID },
			notes: func(c domain.Certificate) string { return c.Notes },
			fields: func(c domain.Certificate) []fieldRow {
				autoRenew := "no"
				if c.AutoRenew {
					autoRenew = "yes"
				}
				return []fieldRow{
					{"Issuer", c.Issuer},
					{"Expires on", c.ExpiresOn},
					{"Auto-renew", autoRenew},
				}
			},
			newItem:  domain.Certificate{},
			listTmpl: "certificates.html", formTmpl: "certificate_form.html",
		},
		resource[domain.Backup]{
			name: "backups", sing: "backup", title: "Backups", heading: "Backup",
			repo:  backups,
			parse: parseBackup,
			label: func(b domain.Backup) string { return b.Source },
			id:    func(b domain.Backup) int64 { return b.ID },
			notes: func(b domain.Backup) string { return b.Notes },
			fields: func(b domain.Backup) []fieldRow {
				return []fieldRow{
					{"Destination", b.Destination},
					{"Frequency", b.Frequency},
					{"Last run", b.LastRun},
				}
			},
			newItem:  domain.Backup{},
			listTmpl: "backups.html", formTmpl: "backup_form.html",
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
