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
	"github.com/Dealisto/almanaut/internal/webhook"
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
	cat          entityCatalog
	tags         *store.TagRepo
	rels         *store.RelationshipRepo
	changelog    *store.ChangelogRepo
	journal      *store.JournalRepo
	customFields *store.CustomFieldRepo
	attachments  *store.AttachmentRepo
	db           *sql.DB
	webhooks     webhook.Dispatcher
}

// resource describes one entity. Only the genuinely entity-specific behavior
// lives here; all plumbing is in the generic methods below.
type resource[T validatable] struct {
	name      string // route base, e.g. "hosts"
	sing      string // singular type key, e.g. "host" (relationships, tags, headings)
	title     string // list page title, e.g. "Hosts"
	heading   string // singular heading prefix, e.g. "Host" → "Host: name"
	repo      crud[T]
	parse     func(get func(string) string, id int64) T // field getter → T; id==0 for create
	label     func(T) string                            // name shown in catalog/detail heading
	id        func(T) int64
	setID     func(item *T, id int64) // writes the id back onto a decoded/parsed T (JSON writes)
	notes     func(T) string
	fields    func(T) []fieldRow                                // detail-page rows
	search    func(T) []string                                  // free-text fields matched by global search
	ipam      func(T) *ipamSection                              // optional; nil for all but network
	children  func(T, entityCatalog) (*childrenSection, error)  // optional; nil for all but Site/Location
	elevation func(T, entityCatalog) (*elevationSection, error) // optional; only Rack
	newItem   T                                                 // zero value with form defaults
	listTmpl  string                                            // "hosts.html"
	formTmpl  string                                            // "host_form.html"
	extras    func() map[string]any                             // form selects (Types, Kinds…); may be nil
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
// needs. Custom field values are folded into Fields (bulk-loaded to avoid N+1)
// so search matches them too.
func (rs resource[T]) searchEntries(cf *store.CustomFieldRepo) ([]searchEntry, error) {
	items, err := rs.repo.List()
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(items))
	for _, it := range items {
		ids = append(ids, rs.id(it))
	}
	cfValues, err := cf.ValuesForEntities(rs.sing, ids)
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
		for _, v := range cfValues[id] {
			fields = append(fields, v.Value)
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

func (rs resource[T]) newForm(d handlerDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		rows, err := d.customFieldFormRows(rs.sing, 0, nil)
		if err != nil {
			serverError(w, req, err)
			return
		}
		render(w, req, rs.formTmpl, formData[T]{
			Title:       "New " + rs.sing,
			Heading:     "New " + rs.sing,
			Action:      rs.basePath(),
			SubmitLabel: "Create",
			Item:        rs.newItem,
			Extras:      withCustomFields(rs.extraData(), rows),
		})
	}
}

func (rs resource[T]) editForm(d handlerDeps) http.HandlerFunc {
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
		rows, err := d.customFieldFormRows(rs.sing, id, nil)
		if err != nil {
			serverError(w, req, err)
			return
		}
		render(w, req, rs.formTmpl, formData[T]{
			Title: "Edit " + rs.sing, Heading: "Edit " + rs.sing,
			Action: fmt.Sprintf("%s/%d", rs.basePath(), id), SubmitLabel: "Save",
			Item: item, Extras: withCustomFields(rs.extraData(), rows),
		})
	}
}

// createEntityTx inserts item, sets its custom field values, and records a
// create in the changelog on the given transaction. item's id is expected to be
// zero; the new id is returned. cf may be nil (no custom field values to set).
// The webhook event for the create is appended to events (dispatched by the
// caller only after the surrounding transaction commits).
func (rs resource[T]) createEntityTx(tx *sql.Tx, d handlerDeps, item T, cf map[int64]string, actor string, events *[]webhook.Event) (int64, error) {
	id, err := rs.repo.CreateTx(tx, item)
	if err != nil {
		return 0, err
	}
	if err := d.customFields.WithTx(tx).SetForEntity(rs.sing, id, cf); err != nil {
		return 0, err
	}
	var zero T
	changes, err := domain.Diff(zero, item)
	if err != nil {
		return 0, err
	}
	now := nowRFC3339()
	if err := d.changelog.WithTx(tx).Create(store.ChangeEvent{
		EntityType: rs.sing, EntityID: id, Label: rs.label(item),
		Action: domain.ActionCreate, Actor: actor,
		Changes: changes, CreatedAt: now,
	}); err != nil {
		return 0, err
	}
	ev, err := webhook.NewEvent(rs.sing, id, webhook.ActionCreated, actor, now, item)
	if err != nil {
		return 0, err
	}
	*events = append(*events, ev)
	return id, nil
}

// createEntity inserts item, sets its custom field values, and records a create
// in the changelog, atomically, then dispatches the webhook event after commit.
// cf may be nil.
func (rs resource[T]) createEntity(d handlerDeps, item T, cf map[int64]string, actor string) (int64, error) {
	var id int64
	var events []webhook.Event
	err := store.WithTx(d.db, func(tx *sql.Tx) error {
		var err error
		id, err = rs.createEntityTx(tx, d, item, cf, actor, &events)
		return err
	})
	if err != nil {
		return id, err
	}
	d.webhooks.Dispatch(events...)
	return id, nil
}

// updateEntityTx overwrites the row identified by rs.id(item), sets its custom
// field values, and records the typed-field diff in the changelog on the given
// transaction. Custom field values are always applied (even when the typed
// fields are unchanged); a no-op typed diff records no changelog entry and no
// webhook event. cf may be nil. The webhook event (when any) is appended to
// events for the caller to dispatch after commit.
func (rs resource[T]) updateEntityTx(tx *sql.Tx, d handlerDeps, item T, cf map[int64]string, actor string, events *[]webhook.Event) error {
	old, err := rs.repo.GetTx(tx, rs.id(item))
	if err != nil {
		return err
	}
	if err := rs.repo.UpdateTx(tx, item); err != nil {
		return err
	}
	if err := d.customFields.WithTx(tx).SetForEntity(rs.sing, rs.id(item), cf); err != nil {
		return err
	}
	changes, err := domain.Diff(old, item)
	if err != nil {
		return err
	}
	if len(changes) == 0 {
		return nil // no-op typed save: nothing worth logging or dispatching
	}
	now := nowRFC3339()
	if err := d.changelog.WithTx(tx).Create(store.ChangeEvent{
		EntityType: rs.sing, EntityID: rs.id(item), Label: rs.label(item),
		Action: domain.ActionUpdate, Actor: actor,
		Changes: changes, CreatedAt: now,
	}); err != nil {
		return err
	}
	ev, err := webhook.NewEvent(rs.sing, rs.id(item), webhook.ActionUpdated, actor, now, item)
	if err != nil {
		return err
	}
	*events = append(*events, ev)
	return nil
}

// updateEntity overwrites the row identified by rs.id(item), sets its custom
// field values, and records the typed-field diff, atomically, then dispatches
// the webhook event (if any) after commit. cf may be nil.
func (rs resource[T]) updateEntity(d handlerDeps, item T, cf map[int64]string, actor string) error {
	var events []webhook.Event
	err := store.WithTx(d.db, func(tx *sql.Tx) error {
		return rs.updateEntityTx(tx, d, item, cf, actor, &events)
	})
	if err != nil {
		return err
	}
	d.webhooks.Dispatch(events...)
	return nil
}

// deleteEntity removes the entity and its relationship/tag/journal/custom-field
// edges and records a delete in the changelog, atomically, then dispatches the
// webhook event after commit.
func (rs resource[T]) deleteEntity(d handlerDeps, id int64, actor string) error {
	var events []webhook.Event
	err := store.WithTx(d.db, func(tx *sql.Tx) error {
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
		if err := d.customFields.WithTx(tx).DeleteByEntity(rs.sing, id); err != nil {
			return err
		}
		if err := d.attachments.WithTx(tx).DeleteByEntity(rs.sing, id); err != nil {
			return err
		}
		now := nowRFC3339()
		if err := d.changelog.WithTx(tx).Create(store.ChangeEvent{
			EntityType: rs.sing, EntityID: id, Label: label,
			Action: domain.ActionDelete, Actor: actor,
			Changes: nil, CreatedAt: now,
		}); err != nil {
			return err
		}
		ev, err := webhook.NewEvent(rs.sing, id, webhook.ActionDeleted, actor, now, nil)
		if err != nil {
			return err
		}
		events = append(events, ev)
		return nil
	})
	if err != nil {
		return err
	}
	d.webhooks.Dispatch(events...)
	return nil
}

func (rs resource[T]) create(d handlerDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		item := rs.parse(req.FormValue, 0)
		if err := item.Validate(); err != nil {
			rs.renderCreateError(w, req, d, item, err)
			return
		}
		cf, err := d.parseCustomFields(rs.sing, req.FormValue)
		if err != nil {
			rs.renderCreateError(w, req, d, item, err)
			return
		}
		if _, err := rs.createEntity(d, item, cf, actor(req)); err != nil {
			serverError(w, req, err)
			return
		}
		http.Redirect(w, req, rs.basePath(), http.StatusSeeOther)
	}
}

// renderCreateError re-renders the create form preserving submitted custom
// field values.
func (rs resource[T]) renderCreateError(w http.ResponseWriter, req *http.Request, d handlerDeps, item T, cause error) {
	rows, err := d.customFieldFormRows(rs.sing, 0, req.FormValue)
	if err != nil {
		serverError(w, req, err)
		return
	}
	render(w, req, rs.formTmpl, formData[T]{
		Title: "New " + rs.sing, Heading: "New " + rs.sing,
		Action: rs.basePath(), SubmitLabel: "Create",
		Item: item, Extras: withCustomFields(rs.extraData(), rows), Error: cause.Error(),
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
			rs.renderUpdateError(w, req, d, id, item, err)
			return
		}
		cf, err := d.parseCustomFields(rs.sing, req.FormValue)
		if err != nil {
			rs.renderUpdateError(w, req, d, id, item, err)
			return
		}
		if err := rs.updateEntity(d, item, cf, actor(req)); err != nil {
			notFoundOrServerError(w, req, rs.sing, err)
			return
		}
		http.Redirect(w, req, rs.basePath(), http.StatusSeeOther)
	}
}

// renderUpdateError re-renders the edit form preserving submitted custom field
// values.
func (rs resource[T]) renderUpdateError(w http.ResponseWriter, req *http.Request, d handlerDeps, id int64, item T, cause error) {
	rows, err := d.customFieldFormRows(rs.sing, id, req.FormValue)
	if err != nil {
		serverError(w, req, err)
		return
	}
	render(w, req, rs.formTmpl, formData[T]{
		Title: "Edit " + rs.sing, Heading: "Edit " + rs.sing,
		Action: fmt.Sprintf("%s/%d", rs.basePath(), id), SubmitLabel: "Save",
		Item: item, Extras: withCustomFields(rs.extraData(), rows), Error: cause.Error(),
	})
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
		var elevation *elevationSection
		if rs.elevation != nil {
			elevation, err = rs.elevation(item, d.cat)
			if err != nil {
				serverError(w, req, err)
				return
			}
		}
		cfValues, err := d.customFields.ListForEntity(rs.sing, id)
		if err != nil {
			serverError(w, req, err)
			return
		}
		atts, err := d.attachments.ListForEntity(rs.sing, id)
		if err != nil {
			serverError(w, req, err)
			return
		}
		attViews := make([]attachmentView, 0, len(atts))
		for _, a := range atts {
			attViews = append(attViews, attachmentView{
				ID: a.ID, Filename: a.Filename, Size: humanizeBytes(a.Size), UploadedAt: a.UploadedAt,
			})
		}
		renderDetailExtra(w, req, d.cat, d.tags, d.rels, d.journal, d.changelog, rs.sing, id,
			rs.heading+": "+rs.label(item), rs.notes(item),
			fmt.Sprintf("%s/%d/edit", rs.basePath(), id), rs.basePath(), rs.fields(item),
			detailExtras{ipam: ipam, children: children, elevation: elevation, customFields: cfValues, attachments: attViews})
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
	r.Get(rs.basePath()+"/new", rs.newForm(d))
	r.Post(rs.basePath(), rs.create(d))
	r.Get(rs.basePath()+"/{id}", rs.show(d))
	r.Get(rs.basePath()+"/{id}/edit", rs.editForm(d))
	r.Post(rs.basePath()+"/{id}", rs.update(d))
	r.Post(rs.basePath()+"/{id}/delete", rs.del(d))
	r.Post(rs.basePath()+"/{id}/journal", rs.addJournal(d))
	r.Post(rs.basePath()+"/{id}/attachments", rs.addAttachment(d))
}

// listJSON writes all entities of this type as a JSON array, each merged with
// its custom_fields.
func (rs resource[T]) listJSON(d handlerDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		items, err := rs.repo.List()
		if err != nil {
			apiServerError(w, req, err)
			return
		}
		ids := make([]int64, 0, len(items))
		for _, it := range items {
			ids = append(ids, rs.id(it))
		}
		cfValues, err := d.customFields.ValuesForEntities(rs.sing, ids)
		if err != nil {
			apiServerError(w, req, err)
			return
		}
		out := make([]json.RawMessage, 0, len(items))
		for _, it := range items {
			buf, err := mergeCustomFields(it, cfValues[rs.id(it)])
			if err != nil {
				apiServerError(w, req, err)
				return
			}
			out = append(out, buf)
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// getJSON writes one entity as JSON merged with its custom_fields, 404 if
// absent, 400 on a malformed id.
func (rs resource[T]) getJSON(d handlerDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
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
		vals, err := d.customFields.ListForEntity(rs.sing, id)
		if err != nil {
			apiServerError(w, req, err)
			return
		}
		writeEntityJSON(w, http.StatusOK, item, vals)
	}
}

// createJSON decodes a JSON entity, validates it, and creates it. The client's
// id (if any) is ignored. Returns 201 with the created entity and a Location.
func (rs resource[T]) createJSON(d handlerDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		var item T
		if err := json.Unmarshal(body, &item); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		var aux struct {
			CustomFields map[string]json.RawMessage `json:"custom_fields"`
		}
		_ = json.Unmarshal(body, &aux) // custom_fields is optional
		cf, err := d.parseJSONCustomFields(rs.sing, aux.CustomFields)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		rs.setID(&item, 0)
		if err := item.Validate(); err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		id, err := rs.createEntity(d, item, cf, actor(req))
		if err != nil {
			apiServerError(w, req, err)
			return
		}
		rs.setID(&item, id)
		// Reload for the response echo only; the create already committed, so a
		// failure here must not turn a successful write into a 500 (and, for
		// this POST, risk a client retry creating a duplicate row).
		vals, _ := d.customFields.ListForEntity(rs.sing, id)
		w.Header().Set("Location", fmt.Sprintf("/api%s/%d", rs.basePath(), id))
		writeEntityJSON(w, http.StatusCreated, item, vals)
	}
}

// updateJSON decodes a JSON entity and full-replaces the row at {id}. The URL id
// is authoritative (any id in the body is overwritten). 404 if the row is absent.
// Custom fields are only modified when a custom_fields object is present in the body; an omitted or empty custom_fields leaves existing values unchanged.
func (rs resource[T]) updateJSON(d handlerDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid id")
			return
		}
		body, err := io.ReadAll(req.Body)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		var item T
		if err := json.Unmarshal(body, &item); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		var aux struct {
			CustomFields map[string]json.RawMessage `json:"custom_fields"`
		}
		_ = json.Unmarshal(body, &aux) // custom_fields is optional
		cf, err := d.parseJSONCustomFields(rs.sing, aux.CustomFields)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		rs.setID(&item, id)
		if err := item.Validate(); err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := rs.updateEntity(d, item, cf, actor(req)); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeJSONError(w, http.StatusNotFound, rs.sing+" not found")
				return
			}
			apiServerError(w, req, err)
			return
		}
		// Reload for the response echo only; the update already committed, so a
		// failure here must not turn a successful write into a 500.
		vals, _ := d.customFields.ListForEntity(rs.sing, id)
		writeEntityJSON(w, http.StatusOK, item, vals)
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
	r.Get(base, rs.listJSON(d))
	r.Post(base, rs.createJSON(d))
	r.Get(base+"/{id}", rs.getJSON(d))
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
	searchEntries(cf *store.CustomFieldRepo) ([]searchEntry, error)
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
	rackID, _ := strconv.ParseInt(get("rack_id"), 10, 64)
	rackPos, _ := strconv.Atoi(strings.TrimSpace(get("rack_position")))
	uHeight, _ := strconv.Atoi(strings.TrimSpace(get("u_height")))
	return domain.Host{
		ID:           id,
		Name:         strings.TrimSpace(get("name")),
		Type:         get("type"),
		OS:           get("os"),
		CPU:          get("cpu"),
		RAM:          get("ram"),
		Disk:         get("disk"),
		Status:       get("status"),
		Notes:        get("notes"),
		IPs:          parseIPs(get("ips")),
		CheckAddress: strings.TrimSpace(get("check_address")),

		RackID:       rackID,
		RackPosition: rackPos,
		UHeight:      uHeight,
	}
}

func parseService(get func(string) string, id int64) domain.Service {
	return domain.Service{
		ID:           id,
		Name:         strings.TrimSpace(get("name")),
		Kind:         get("kind"),
		URL:          get("url"),
		Ports:        get("ports"),
		Category:     get("category"),
		Notes:        get("notes"),
		CheckAddress: strings.TrimSpace(get("check_address")),
	}
}

func parseNetwork(get func(string) string, id int64) domain.Network {
	vlanID, _ := strconv.ParseInt(get("vlan_id"), 10, 64)
	return domain.Network{
		ID:      id,
		Name:    strings.TrimSpace(get("name")),
		CIDR:    strings.TrimSpace(get("cidr")),
		VLANID:  vlanID,
		Gateway: strings.TrimSpace(get("gateway")),
		Notes:   get("notes"),
	}
}

func parseVLAN(get func(string) string, id int64) domain.VLAN {
	vid, _ := strconv.Atoi(strings.TrimSpace(get("vid")))
	return domain.VLAN{
		ID:    id,
		Name:  strings.TrimSpace(get("name")),
		VID:   vid,
		Notes: get("notes"),
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
	rackID, _ := strconv.ParseInt(get("rack_id"), 10, 64)
	rackPos, _ := strconv.Atoi(strings.TrimSpace(get("rack_position")))
	uHeight, _ := strconv.Atoi(strings.TrimSpace(get("u_height")))
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

		RackID:       rackID,
		RackPosition: rackPos,
		UHeight:      uHeight,
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

func parseContact(get func(string) string, id int64) domain.Contact {
	return domain.Contact{
		ID:           id,
		Name:         strings.TrimSpace(get("name")),
		Email:        strings.TrimSpace(get("email")),
		Phone:        strings.TrimSpace(get("phone")),
		Role:         strings.TrimSpace(get("role")),
		Organization: strings.TrimSpace(get("organization")),
		Notes:        get("notes"),
	}
}

func parseSite(get func(string) string, id int64) domain.Site {
	return domain.Site{
		ID:      id,
		Name:    strings.TrimSpace(get("name")),
		Address: strings.TrimSpace(get("address")),
		Notes:   get("notes"),
	}
}

func parseLocation(get func(string) string, id int64) domain.Location {
	siteID, _ := strconv.ParseInt(get("site_id"), 10, 64)
	return domain.Location{
		ID:     id,
		Name:   strings.TrimSpace(get("name")),
		SiteID: siteID,
		Notes:  get("notes"),
	}
}

func parseRack(get func(string) string, id int64) domain.Rack {
	locationID, _ := strconv.ParseInt(get("location_id"), 10, 64)
	uHeight, _ := strconv.Atoi(strings.TrimSpace(get("u_height")))
	return domain.Rack{
		ID:         id,
		Name:       strings.TrimSpace(get("name")),
		LocationID: locationID,
		UHeight:    uHeight,
		Notes:      get("notes"),
	}
}

func parseReservation(get func(string) string, id int64) domain.Reservation {
	networkID, _ := strconv.ParseInt(get("network_id"), 10, 64)
	return domain.Reservation{
		ID:        id,
		NetworkID: networkID,
		Name:      strings.TrimSpace(get("name")),
		StartIP:   strings.TrimSpace(get("start_ip")),
		EndIP:     strings.TrimSpace(get("end_ip")),
		Notes:     get("notes"),
	}
}
