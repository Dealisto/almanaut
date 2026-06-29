package web

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
	"github.com/go-chi/chi/v5"
)

// crud is the subset of every *store.XRepo that the generic handlers need.
// Each store repo satisfies it directly (uniform Create/Get/List/Update/Delete).
type crud[T any] interface {
	List() ([]T, error)
	Get(id int64) (T, error)
	Create(T) (int64, error)
	Update(T) error
	Delete(id int64) error
}

// validatable is satisfied by every domain entity (value-receiver Validate).
type validatable interface{ Validate() error }

// listData / formData replace the per-entity page-data structs.
type listData[T any] struct {
	Title string
	Items []T
}

type formData[T any] struct {
	Title, Heading, Action, SubmitLabel, Error string
	Item                                       T
	Extras                                     map[string]any
}

// handlerDeps bundles the cross-entity dependencies the show/delete handlers need,
// replacing the long parameter lists on the old per-entity handlers.
type handlerDeps struct {
	cat  entityCatalog
	tags *store.TagRepo
	rels *store.RelationshipRepo
}

// resource describes one entity. Only the genuinely entity-specific behavior
// lives here; all plumbing is in the generic methods below.
type resource[T validatable] struct {
	name     string // route base, e.g. "hosts"
	sing     string // singular type key, e.g. "host" (relationships, tags, headings)
	title    string // list page title, e.g. "Hosts"
	heading  string // singular heading prefix, e.g. "Host" → "Host: name"
	repo     crud[T]
	parse    func(r *http.Request, id int64) T // form → T; id==0 for create
	label    func(T) string                    // name shown in catalog/detail heading
	id       func(T) int64
	notes    func(T) string
	fields   func(T) []fieldRow    // detail-page rows
	ipam     func(T) *ipamSection  // optional; nil for all but network
	newItem  T                     // zero value with form defaults
	listTmpl string                // "hosts.html"
	formTmpl string                // "host_form.html"
	extras   func() map[string]any // form selects (Types, Kinds…); may be nil
}

func (rs resource[T]) singular() string { return rs.sing }

// options lists this resource's entities as relationship/catalog options.
func (rs resource[T]) options() ([]entityOption, error) {
	items, err := rs.repo.List()
	if err != nil {
		return nil, err
	}
	opts := make([]entityOption, 0, len(items))
	for _, it := range items {
		opts = append(opts, entityOptionOf(rs.sing, rs.id(it), rs.label(it)))
	}
	return opts, nil
}

func (rs resource[T]) extraData() map[string]any {
	if rs.extras == nil {
		return nil
	}
	return rs.extras()
}

func (rs resource[T]) basePath() string { return "/" + rs.name }

func (rs resource[T]) idParam(w http.ResponseWriter, req *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return 0, false
	}
	return id, true
}

func (rs resource[T]) list(w http.ResponseWriter, req *http.Request) {
	items, err := rs.repo.List()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	render(w, rs.listTmpl, listData[T]{Title: rs.title, Items: items})
}

func (rs resource[T]) newForm(w http.ResponseWriter, req *http.Request) {
	render(w, rs.formTmpl, formData[T]{
		Title:       "New " + rs.sing,
		Heading:     "New " + rs.sing,
		Action:      rs.basePath(),
		SubmitLabel: "Create",
		Item:        rs.newItem,
		Extras:      rs.extraData(),
	})
}

func (rs resource[T]) create(w http.ResponseWriter, req *http.Request) {
	item := rs.parse(req, 0)
	if err := item.Validate(); err != nil {
		render(w, rs.formTmpl, formData[T]{
			Title: "New " + rs.sing, Heading: "New " + rs.sing,
			Action: rs.basePath(), SubmitLabel: "Create",
			Item: item, Extras: rs.extraData(), Error: err.Error(),
		})
		return
	}
	if _, err := rs.repo.Create(item); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, req, rs.basePath(), http.StatusSeeOther)
}

func (rs resource[T]) editForm(w http.ResponseWriter, req *http.Request) {
	id, ok := rs.idParam(w, req)
	if !ok {
		return
	}
	item, err := rs.repo.Get(id)
	if err != nil {
		http.Error(w, rs.sing+" not found", http.StatusNotFound)
		return
	}
	render(w, rs.formTmpl, formData[T]{
		Title: "Edit " + rs.sing, Heading: "Edit " + rs.sing,
		Action: fmt.Sprintf("%s/%d", rs.basePath(), id), SubmitLabel: "Save",
		Item: item, Extras: rs.extraData(),
	})
}

func (rs resource[T]) update(w http.ResponseWriter, req *http.Request) {
	id, ok := rs.idParam(w, req)
	if !ok {
		return
	}
	item := rs.parse(req, id)
	if err := item.Validate(); err != nil {
		render(w, rs.formTmpl, formData[T]{
			Title: "Edit " + rs.sing, Heading: "Edit " + rs.sing,
			Action: fmt.Sprintf("%s/%d", rs.basePath(), id), SubmitLabel: "Save",
			Item: item, Extras: rs.extraData(), Error: err.Error(),
		})
		return
	}
	if err := rs.repo.Update(item); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, req, rs.basePath(), http.StatusSeeOther)
}

func (rs resource[T]) show(d handlerDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, ok := rs.idParam(w, req)
		if !ok {
			return
		}
		item, err := rs.repo.Get(id)
		if err != nil {
			http.Error(w, rs.sing+" not found", http.StatusNotFound)
			return
		}
		var ipam *ipamSection
		if rs.ipam != nil {
			ipam = rs.ipam(item)
		}
		renderDetailExtra(w, d.cat, d.tags, d.rels, rs.sing, id,
			rs.heading+": "+rs.label(item), rs.notes(item),
			fmt.Sprintf("%s/%d/edit", rs.basePath(), id), rs.fields(item), ipam)
	}
}

func (rs resource[T]) del(d handlerDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, ok := rs.idParam(w, req)
		if !ok {
			return
		}
		if err := rs.repo.Delete(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := d.rels.DeleteByEntity(rs.sing, id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := d.tags.DeleteByEntity(rs.sing, id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, req, rs.basePath(), http.StatusSeeOther)
	}
}

// mount wires all seven routes for this entity.
func (rs resource[T]) mount(r chi.Router, d handlerDeps) {
	r.Get(rs.basePath(), rs.list)
	r.Get(rs.basePath()+"/new", rs.newForm)
	r.Post(rs.basePath(), rs.create)
	r.Get(rs.basePath()+"/{id}", rs.show(d))
	r.Get(rs.basePath()+"/{id}/edit", rs.editForm)
	r.Post(rs.basePath()+"/{id}", rs.update)
	r.Post(rs.basePath()+"/{id}/delete", rs.del(d))
}

// mountable lets New store heterogeneous resource[T] values in one slice.
type mountable interface {
	mount(r chi.Router, d handlerDeps)
	options() ([]entityOption, error)
	singular() string
}

func parseHost(r *http.Request, id int64) domain.Host {
	return domain.Host{
		ID:     id,
		Name:   strings.TrimSpace(r.FormValue("name")),
		Type:   r.FormValue("type"),
		OS:     r.FormValue("os"),
		CPU:    r.FormValue("cpu"),
		RAM:    r.FormValue("ram"),
		Disk:   r.FormValue("disk"),
		Status: r.FormValue("status"),
		Notes:  r.FormValue("notes"),
		IPs:    parseIPs(r.FormValue("ips")),
	}
}

func parseService(r *http.Request, id int64) domain.Service {
	return domain.Service{
		ID:       id,
		Name:     strings.TrimSpace(r.FormValue("name")),
		Kind:     r.FormValue("kind"),
		URL:      r.FormValue("url"),
		Ports:    r.FormValue("ports"),
		Category: r.FormValue("category"),
		Notes:    r.FormValue("notes"),
	}
}

func parseNetwork(r *http.Request, id int64) domain.Network {
	return domain.Network{
		ID:      id,
		Name:    strings.TrimSpace(r.FormValue("name")),
		CIDR:    strings.TrimSpace(r.FormValue("cidr")),
		VLAN:    r.FormValue("vlan"),
		Gateway: strings.TrimSpace(r.FormValue("gateway")),
		Notes:   r.FormValue("notes"),
	}
}

func parseDomain(r *http.Request, id int64) domain.Domain {
	return domain.Domain{
		ID:       id,
		FQDN:     strings.TrimSpace(r.FormValue("fqdn")),
		Provider: strings.TrimSpace(r.FormValue("provider")),
		Notes:    r.FormValue("notes"),
	}
}

func parseCertificate(r *http.Request, id int64) domain.Certificate {
	return domain.Certificate{
		ID:        id,
		Subject:   strings.TrimSpace(r.FormValue("subject")),
		Issuer:    strings.TrimSpace(r.FormValue("issuer")),
		ExpiresOn: strings.TrimSpace(r.FormValue("expires_on")),
		AutoRenew: r.FormValue("auto_renew") == "on",
		Notes:     r.FormValue("notes"),
	}
}

func parseBackup(r *http.Request, id int64) domain.Backup {
	return domain.Backup{
		ID:          id,
		Source:      strings.TrimSpace(r.FormValue("source")),
		Destination: strings.TrimSpace(r.FormValue("destination")),
		Frequency:   strings.TrimSpace(r.FormValue("frequency")),
		LastRun:     strings.TrimSpace(r.FormValue("last_run")),
		Notes:       r.FormValue("notes"),
	}
}

func parseHardware(r *http.Request, id int64) domain.Hardware {
	return domain.Hardware{
		ID:           id,
		Name:         strings.TrimSpace(r.FormValue("name")),
		Kind:         strings.TrimSpace(r.FormValue("kind")),
		Manufacturer: strings.TrimSpace(r.FormValue("manufacturer")),
		Model:        strings.TrimSpace(r.FormValue("model")),
		Serial:       strings.TrimSpace(r.FormValue("serial")),
		Location:     strings.TrimSpace(r.FormValue("location")),
		PurchaseDate: strings.TrimSpace(r.FormValue("purchase_date")),
		WarrantyEnd:  strings.TrimSpace(r.FormValue("warranty_end")),
		Status:       strings.TrimSpace(r.FormValue("status")),
		Notes:        r.FormValue("notes"),
	}
}

func parseSubscription(r *http.Request, id int64) domain.Subscription {
	return domain.Subscription{
		ID:           id,
		Name:         strings.TrimSpace(r.FormValue("name")),
		Kind:         strings.TrimSpace(r.FormValue("kind")),
		Provider:     strings.TrimSpace(r.FormValue("provider")),
		Amount:       strings.TrimSpace(r.FormValue("amount")),
		Currency:     strings.TrimSpace(r.FormValue("currency")),
		BillingCycle: strings.TrimSpace(r.FormValue("billing_cycle")),
		RenewalDate:  strings.TrimSpace(r.FormValue("renewal_date")),
		AutoRenew:    r.FormValue("auto_renew") == "on",
		Status:       strings.TrimSpace(r.FormValue("status")),
		Notes:        r.FormValue("notes"),
	}
}

func parseAccount(r *http.Request, id int64) domain.Account {
	return domain.Account{
		ID:              id,
		Name:            strings.TrimSpace(r.FormValue("name")),
		Kind:            strings.TrimSpace(r.FormValue("kind")),
		Username:        strings.TrimSpace(r.FormValue("username")),
		PasswordManager: strings.TrimSpace(r.FormValue("password_manager")),
		SecretRef:       strings.TrimSpace(r.FormValue("secret_ref")),
		URL:             strings.TrimSpace(r.FormValue("url")),
		Status:          strings.TrimSpace(r.FormValue("status")),
		Notes:           r.FormValue("notes"),
	}
}
