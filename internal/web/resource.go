package web

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
	"github.com/go-chi/chi/v5"
)

// crud is the subset of every *store.XRepo that the generic handlers need.
// Each store repo satisfies it directly (uniform Create/Get/List/Update/Delete,
// plus DeleteTx so deletion can run inside the cascade transaction).
type crud[T any] interface {
	List() ([]T, error)
	Get(id int64) (T, error)
	Create(T) (int64, error)
	Update(T) error
	Delete(id int64) error
	DeleteTx(tx *sql.Tx, id int64) error
	CreateTx(tx *sql.Tx, v T) (int64, error)
	UpdateTx(tx *sql.Tx, v T) error
	GetTx(tx *sql.Tx, id int64) (T, error)
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
	cat       entityCatalog
	tags      *store.TagRepo
	rels      *store.RelationshipRepo
	changelog *store.ChangelogRepo
	journal   *store.JournalRepo
	db        *sql.DB
}

// resource describes one entity. Only the genuinely entity-specific behavior
// lives here; all plumbing is in the generic methods below.
type resource[T validatable] struct {
	name     string // route base, e.g. "hosts"
	sing     string // singular type key, e.g. "host" (relationships, tags, headings)
	title    string // list page title, e.g. "Hosts"
	heading  string // singular heading prefix, e.g. "Host" → "Host: name"
	repo     crud[T]
	parse    func(get func(string) string, id int64) T // field getter → T; id==0 for create
	label    func(T) string                            // name shown in catalog/detail heading
	id       func(T) int64
	setID    func(item *T, id int64) // writes the id back onto a decoded/parsed T (JSON writes)
	notes    func(T) string
	fields   func(T) []fieldRow                               // detail-page rows
	search   func(T) []string                                 // free-text fields matched by global search
	ipam     func(T) *ipamSection                             // optional; nil for all but network
	children func(T, entityCatalog) (*childrenSection, error) // optional; nil for all but Site/Location
	newItem  T                                                // zero value with form defaults
	listTmpl string                                           // "hosts.html"
	formTmpl string                                           // "host_form.html"
	extras   func() map[string]any                            // form selects (Types, Kinds…); may be nil
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

// searchHeading is the group title this resource's hits appear under on the
// search results page.
func (rs resource[T]) searchHeading() string { return rs.title }

// searchEntries projects every entity into the form the global search handler
// needs: its label, detail-page path, and the free-text fields to match against.
func (rs resource[T]) searchEntries() ([]searchEntry, error) {
	items, err := rs.repo.List()
	if err != nil {
		return nil, err
	}
	out := make([]searchEntry, 0, len(items))
	for _, it := range items {
		id := rs.id(it)
		var fields []string
		if rs.search != nil {
			fields = rs.search(it)
		}
		out = append(out, searchEntry{
			Type:   rs.sing,
			ID:     id,
			Label:  rs.label(it),
			Path:   fmt.Sprintf("%s/%d", rs.basePath(), id),
			Fields: fields,
		})
	}
	return out, nil
}

func (rs resource[T]) idParam(w http.ResponseWriter, req *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return 0, false
	}
	return id, true
}

// notFoundOrServerError writes a 404 for a missing entity (store.ErrNotFound)
// and otherwise a logged 500, so a real database failure is never masked as
// "not found".
func notFoundOrServerError(w http.ResponseWriter, req *http.Request, sing string, err error) {
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, sing+" not found", http.StatusNotFound)
		return
	}
	serverError(w, req, err)
}

func (rs resource[T]) list(w http.ResponseWriter, req *http.Request) {
	items, err := rs.repo.List()
	if err != nil {
		serverError(w, req, err)
		return
	}
	render(w, req, rs.listTmpl, listData[T]{Title: rs.title, Items: items})
}

func (rs resource[T]) newForm(w http.ResponseWriter, req *http.Request) {
	render(w, req, rs.formTmpl, formData[T]{
		Title:       "New " + rs.sing,
		Heading:     "New " + rs.sing,
		Action:      rs.basePath(),
		SubmitLabel: "Create",
		Item:        rs.newItem,
		Extras:      rs.extraData(),
	})
}

// createEntityTx inserts item and records a create in the changelog on the given
// transaction. item's id is expected to be zero; the new id is returned.
func (rs resource[T]) createEntityTx(tx *sql.Tx, d handlerDeps, item T, actor string) (int64, error) {
	id, err := rs.repo.CreateTx(tx, item)
	if err != nil {
		return 0, err
	}
	var zero T
	changes, err := domain.Diff(zero, item)
	if err != nil {
		return 0, err
	}
	err = d.changelog.WithTx(tx).Create(store.ChangeEvent{
		EntityType: rs.sing, EntityID: id, Label: rs.label(item),
		Action: domain.ActionCreate, Actor: actor,
		Changes: changes, CreatedAt: nowRFC3339(),
	})
	return id, err
}

// createEntity inserts item and records a create in the changelog, atomically.
// The caller supplies actor (username) so both the HTML and JSON paths attribute
// correctly. item's id is expected to be zero; the new id is returned.
func (rs resource[T]) createEntity(d handlerDeps, item T, actor string) (int64, error) {
	var id int64
	err := store.WithTx(d.db, func(tx *sql.Tx) error {
		var err error
		id, err = rs.createEntityTx(tx, d, item, actor)
		return err
	})
	return id, err
}

// updateEntityTx overwrites the row identified by rs.id(item) and records the
// diff in the changelog on the given transaction. A no-op edit (empty diff)
// records nothing.
func (rs resource[T]) updateEntityTx(tx *sql.Tx, d handlerDeps, item T, actor string) error {
	old, err := rs.repo.GetTx(tx, rs.id(item))
	if err != nil {
		return err
	}
	if err := rs.repo.UpdateTx(tx, item); err != nil {
		return err
	}
	changes, err := domain.Diff(old, item)
	if err != nil {
		return err
	}
	if len(changes) == 0 {
		return nil // no-op save: nothing worth logging
	}
	return d.changelog.WithTx(tx).Create(store.ChangeEvent{
		EntityType: rs.sing, EntityID: rs.id(item), Label: rs.label(item),
		Action: domain.ActionUpdate, Actor: actor,
		Changes: changes, CreatedAt: nowRFC3339(),
	})
}

// updateEntity overwrites the row identified by rs.id(item) and records the diff
// in the changelog, atomically. A no-op edit (empty diff) records nothing.
func (rs resource[T]) updateEntity(d handlerDeps, item T, actor string) error {
	return store.WithTx(d.db, func(tx *sql.Tx) error {
		return rs.updateEntityTx(tx, d, item, actor)
	})
}

// deleteEntity removes the entity and its relationship/tag/journal edges and
// records a delete in the changelog, atomically.
func (rs resource[T]) deleteEntity(d handlerDeps, id int64, actor string) error {
	return store.WithTx(d.db, func(tx *sql.Tx) error {
		item, err := rs.repo.GetTx(tx, id)
		if err != nil {
			return err
		}
		label := rs.label(item)
		if err := rs.repo.DeleteTx(tx, id); err != nil {
			return err
		}
		if err := d.rels.WithTx(tx).DeleteByEntity(rs.sing, id); err != nil {
			return err
		}
		if err := d.tags.WithTx(tx).DeleteByEntity(rs.sing, id); err != nil {
			return err
		}
		if err := d.journal.WithTx(tx).DeleteByEntity(rs.sing, id); err != nil {
			return err
		}
		return d.changelog.WithTx(tx).Create(store.ChangeEvent{
			EntityType: rs.sing, EntityID: id, Label: label,
			Action: domain.ActionDelete, Actor: actor,
			Changes: nil, CreatedAt: nowRFC3339(),
		})
	})
}

func (rs resource[T]) create(d handlerDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		item := rs.parse(req.FormValue, 0)
		if err := item.Validate(); err != nil {
			render(w, req, rs.formTmpl, formData[T]{
				Title: "New " + rs.sing, Heading: "New " + rs.sing,
				Action: rs.basePath(), SubmitLabel: "Create",
				Item: item, Extras: rs.extraData(), Error: err.Error(),
			})
			return
		}
		if _, err := rs.createEntity(d, item, actor(req)); err != nil {
			serverError(w, req, err)
			return
		}
		http.Redirect(w, req, rs.basePath(), http.StatusSeeOther)
	}
}

func (rs resource[T]) editForm(w http.ResponseWriter, req *http.Request) {
	id, ok := rs.idParam(w, req)
	if !ok {
		return
	}
	item, err := rs.repo.Get(id)
	if err != nil {
		notFoundOrServerError(w, req, rs.sing, err)
		return
	}
	render(w, req, rs.formTmpl, formData[T]{
		Title: "Edit " + rs.sing, Heading: "Edit " + rs.sing,
		Action: fmt.Sprintf("%s/%d", rs.basePath(), id), SubmitLabel: "Save",
		Item: item, Extras: rs.extraData(),
	})
}

func (rs resource[T]) update(d handlerDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, ok := rs.idParam(w, req)
		if !ok {
			return
		}
		item := rs.parse(req.FormValue, id)
		if err := item.Validate(); err != nil {
			render(w, req, rs.formTmpl, formData[T]{
				Title: "Edit " + rs.sing, Heading: "Edit " + rs.sing,
				Action: fmt.Sprintf("%s/%d", rs.basePath(), id), SubmitLabel: "Save",
				Item: item, Extras: rs.extraData(), Error: err.Error(),
			})
			return
		}
		if err := rs.updateEntity(d, item, actor(req)); err != nil {
			notFoundOrServerError(w, req, rs.sing, err)
			return
		}
		http.Redirect(w, req, rs.basePath(), http.StatusSeeOther)
	}
}

func (rs resource[T]) show(d handlerDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, ok := rs.idParam(w, req)
		if !ok {
			return
		}
		item, err := rs.repo.Get(id)
		if err != nil {
			notFoundOrServerError(w, req, rs.sing, err)
			return
		}
		var ipam *ipamSection
		if rs.ipam != nil {
			ipam = rs.ipam(item)
		}
		var children *childrenSection
		if rs.children != nil {
			children, err = rs.children(item, d.cat)
			if err != nil {
				serverError(w, req, err)
				return
			}
		}
		renderDetailExtra(w, req, d.cat, d.tags, d.rels, d.journal, d.changelog, rs.sing, id,
			rs.heading+": "+rs.label(item), rs.notes(item),
			fmt.Sprintf("%s/%d/edit", rs.basePath(), id), rs.basePath(), rs.fields(item),
			detailExtras{ipam: ipam, children: children})
	}
}

func (rs resource[T]) del(d handlerDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, ok := rs.idParam(w, req)
		if !ok {
			return
		}
		if err := rs.deleteEntity(d, id, actor(req)); err != nil {
			notFoundOrServerError(w, req, rs.sing, err)
			return
		}
		http.Redirect(w, req, rs.basePath(), http.StatusSeeOther)
	}
}

// mount wires all seven routes for this entity.
func (rs resource[T]) mount(r chi.Router, d handlerDeps) {
	r.Get(rs.basePath(), rs.list)
	r.Get(rs.basePath()+"/new", rs.newForm)
	r.Post(rs.basePath(), rs.create(d))
	r.Get(rs.basePath()+"/{id}", rs.show(d))
	r.Get(rs.basePath()+"/{id}/edit", rs.editForm)
	r.Post(rs.basePath()+"/{id}", rs.update(d))
	r.Post(rs.basePath()+"/{id}/delete", rs.del(d))
	r.Post(rs.basePath()+"/{id}/journal", rs.addJournal(d))
}

// listJSON writes all entities of this type as a JSON array.
func (rs resource[T]) listJSON(w http.ResponseWriter, req *http.Request) {
	items, err := rs.repo.List()
	if err != nil {
		apiServerError(w, req, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// getJSON writes one entity as JSON, 404 if absent, 400 on a malformed id.
func (rs resource[T]) getJSON(w http.ResponseWriter, req *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid id")
		return
	}
	item, err := rs.repo.Get(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeJSONError(w, http.StatusNotFound, rs.sing+" not found")
			return
		}
		apiServerError(w, req, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// createJSON decodes a JSON entity, validates it, and creates it. The client's
// id (if any) is ignored. Returns 201 with the created entity and a Location.
func (rs resource[T]) createJSON(d handlerDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		var item T
		if err := json.NewDecoder(req.Body).Decode(&item); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		rs.setID(&item, 0)
		if err := item.Validate(); err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		id, err := rs.createEntity(d, item, actor(req))
		if err != nil {
			apiServerError(w, req, err)
			return
		}
		rs.setID(&item, id)
		w.Header().Set("Location", fmt.Sprintf("/api%s/%d", rs.basePath(), id))
		writeJSON(w, http.StatusCreated, item)
	}
}

// updateJSON decodes a JSON entity and full-replaces the row at {id}. The URL id
// is authoritative (any id in the body is overwritten). 404 if the row is absent.
func (rs resource[T]) updateJSON(d handlerDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid id")
			return
		}
		var item T
		if err := json.NewDecoder(req.Body).Decode(&item); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		rs.setID(&item, id)
		if err := item.Validate(); err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := rs.updateEntity(d, item, actor(req)); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeJSONError(w, http.StatusNotFound, rs.sing+" not found")
				return
			}
			apiServerError(w, req, err)
			return
		}
		writeJSON(w, http.StatusOK, item)
	}
}

// deleteJSON removes the entity at {id}. 204 on success, 404 if absent.
func (rs resource[T]) deleteJSON(d handlerDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid id")
			return
		}
		if err := rs.deleteEntity(d, id, actor(req)); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeJSONError(w, http.StatusNotFound, rs.sing+" not found")
				return
			}
			apiServerError(w, req, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// mountAPI registers this resource's JSON routes (read + write).
func (rs resource[T]) mountAPI(r chi.Router, d handlerDeps) {
	base := "/api" + rs.basePath()
	r.Get(base, rs.listJSON)
	r.Post(base, rs.createJSON(d))
	r.Get(base+"/{id}", rs.getJSON)
	r.Put(base+"/{id}", rs.updateJSON(d))
	r.Delete(base+"/{id}", rs.deleteJSON(d))
}

// mountable lets New store heterogeneous resource[T] values in one slice.
type mountable interface {
	mount(r chi.Router, d handlerDeps)
	mountAPI(r chi.Router, d handlerDeps)
	options() ([]entityOption, error)
	singular() string
	basePath() string
	searchHeading() string
	searchEntries() ([]searchEntry, error)
	importCSV(d handlerDeps, r io.Reader, actor string) (int, int, []string, error)
}

// parseFormBool interprets a checkbox ("on") or a CSV-style boolean cell.
func parseFormBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "on", "true", "1", "yes", "y":
		return true
	}
	return false
}

func parseHost(get func(string) string, id int64) domain.Host {
	return domain.Host{
		ID:     id,
		Name:   strings.TrimSpace(get("name")),
		Type:   get("type"),
		OS:     get("os"),
		CPU:    get("cpu"),
		RAM:    get("ram"),
		Disk:   get("disk"),
		Status: get("status"),
		Notes:  get("notes"),
		IPs:    parseIPs(get("ips")),
	}
}

func parseService(get func(string) string, id int64) domain.Service {
	return domain.Service{
		ID:       id,
		Name:     strings.TrimSpace(get("name")),
		Kind:     get("kind"),
		URL:      get("url"),
		Ports:    get("ports"),
		Category: get("category"),
		Notes:    get("notes"),
	}
}

func parseNetwork(get func(string) string, id int64) domain.Network {
	return domain.Network{
		ID:      id,
		Name:    strings.TrimSpace(get("name")),
		CIDR:    strings.TrimSpace(get("cidr")),
		VLAN:    get("vlan"),
		Gateway: strings.TrimSpace(get("gateway")),
		Notes:   get("notes"),
	}
}

func parseDomain(get func(string) string, id int64) domain.Domain {
	return domain.Domain{
		ID:       id,
		FQDN:     strings.TrimSpace(get("fqdn")),
		Provider: strings.TrimSpace(get("provider")),
		Notes:    get("notes"),
	}
}

func parseCertificate(get func(string) string, id int64) domain.Certificate {
	return domain.Certificate{
		ID:        id,
		Subject:   strings.TrimSpace(get("subject")),
		Issuer:    strings.TrimSpace(get("issuer")),
		ExpiresOn: strings.TrimSpace(get("expires_on")),
		AutoRenew: parseFormBool(get("auto_renew")),
		Notes:     get("notes"),
	}
}

func parseBackup(get func(string) string, id int64) domain.Backup {
	return domain.Backup{
		ID:          id,
		Source:      strings.TrimSpace(get("source")),
		Destination: strings.TrimSpace(get("destination")),
		Frequency:   strings.TrimSpace(get("frequency")),
		LastRun:     strings.TrimSpace(get("last_run")),
		Notes:       get("notes"),
	}
}

func parseHardware(get func(string) string, id int64) domain.Hardware {
	return domain.Hardware{
		ID:           id,
		Name:         strings.TrimSpace(get("name")),
		Kind:         strings.TrimSpace(get("kind")),
		Manufacturer: strings.TrimSpace(get("manufacturer")),
		Model:        strings.TrimSpace(get("model")),
		Serial:       strings.TrimSpace(get("serial")),
		Location:     strings.TrimSpace(get("location")),
		PurchaseDate: strings.TrimSpace(get("purchase_date")),
		WarrantyEnd:  strings.TrimSpace(get("warranty_end")),
		Status:       strings.TrimSpace(get("status")),
		Notes:        get("notes"),
	}
}

func parseSubscription(get func(string) string, id int64) domain.Subscription {
	return domain.Subscription{
		ID:           id,
		Name:         strings.TrimSpace(get("name")),
		Kind:         strings.TrimSpace(get("kind")),
		Provider:     strings.TrimSpace(get("provider")),
		Amount:       strings.TrimSpace(get("amount")),
		Currency:     strings.TrimSpace(get("currency")),
		BillingCycle: strings.TrimSpace(get("billing_cycle")),
		RenewalDate:  strings.TrimSpace(get("renewal_date")),
		AutoRenew:    parseFormBool(get("auto_renew")),
		Status:       strings.TrimSpace(get("status")),
		Notes:        get("notes"),
	}
}

func parseAccount(get func(string) string, id int64) domain.Account {
	return domain.Account{
		ID:              id,
		Name:            strings.TrimSpace(get("name")),
		Kind:            strings.TrimSpace(get("kind")),
		Username:        strings.TrimSpace(get("username")),
		PasswordManager: strings.TrimSpace(get("password_manager")),
		SecretRef:       strings.TrimSpace(get("secret_ref")),
		URL:             strings.TrimSpace(get("url")),
		Status:          strings.TrimSpace(get("status")),
		Notes:           get("notes"),
	}
}
