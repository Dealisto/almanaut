package web

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
	"github.com/Dealisto/almanaut/internal/webhook"
)

// allWebhookEvents is the fixed set of delivery actions offered as filters.
var allWebhookEvents = []string{webhook.ActionCreated, webhook.ActionUpdated, webhook.ActionDeleted}

// webhookFormData drives the shared create/edit field partial.
type webhookFormData struct {
	URL           string
	EntityTypes   []string        // all available types (checkbox list)
	Events        []string        // all available events (checkbox list)
	CheckedTypes  map[string]bool // which type checkboxes are pre-checked
	CheckedEvents map[string]bool // which event checkboxes are pre-checked
}

type webhooksPageData struct {
	Title     string
	Webhooks  []domain.Webhook
	Form      webhookFormData // empty create form
	Error     string
	NewSecret string // generated secret, shown once immediately after create
}

func emptyWebhookForm() webhookFormData {
	return webhookFormData{
		URL: "", EntityTypes: domain.EntityTypes, Events: allWebhookEvents,
		CheckedTypes: map[string]bool{}, CheckedEvents: map[string]bool{},
	}
}

func renderWebhooks(w http.ResponseWriter, r *http.Request, repo *store.WebhookRepo, errMsg, newSecret string) {
	list, err := repo.List()
	if err != nil {
		serverError(w, r, err)
		return
	}
	render(w, r, "webhooks.html", webhooksPageData{
		Title: "Webhooks", Webhooks: list, Form: emptyWebhookForm(),
		Error: errMsg, NewSecret: newSecret,
	})
}

func listWebhooks(repo *store.WebhookRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { renderWebhooks(w, r, repo, "", "") }
}

func createWebhook(repo *store.WebhookRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		url, types, events, err := parseWebhookForm(r)
		if err != nil {
			renderWebhooks(w, r, repo, err.Error(), "")
			return
		}
		secret, gerr := newWebhookSecret()
		if gerr != nil {
			serverError(w, r, gerr)
			return
		}
		if _, err := repo.Create(domain.Webhook{
			URL: url, Secret: secret, Enabled: true,
			EntityTypes: types, Events: events, CreatedAt: nowRFC3339(),
		}); err != nil {
			serverError(w, r, err)
			return
		}
		renderWebhooks(w, r, repo, "", secret) // reveal once
	}
}

type webhookEditData struct {
	Title string
	ID    int64
	Form  webhookFormData
	Error string
}

// checkedSet turns a filter slice into a lookup for pre-checking checkboxes.
func checkedSet(values []string) map[string]bool {
	m := make(map[string]bool, len(values))
	for _, v := range values {
		m[v] = true
	}
	return m
}

func editWebhookForm(repo *store.WebhookRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := webhookIDParam(w, r)
		if !ok {
			return
		}
		wh, err := repo.Get(id)
		if err != nil {
			notFoundOrServerError(w, r, "webhook", err)
			return
		}
		render(w, r, "webhook_edit.html", webhookEditData{
			Title: "Edit webhook", ID: wh.ID,
			Form: webhookFormData{
				URL: wh.URL, EntityTypes: domain.EntityTypes, Events: allWebhookEvents,
				CheckedTypes: checkedSet(wh.EntityTypes), CheckedEvents: checkedSet(wh.Events),
			},
		})
	}
}

func updateWebhook(repo *store.WebhookRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := webhookIDParam(w, r)
		if !ok {
			return
		}
		wh, err := repo.Get(id)
		if err != nil {
			notFoundOrServerError(w, r, "webhook", err)
			return
		}
		url, types, events, perr := parseWebhookForm(r)
		if perr != nil {
			render(w, r, "webhook_edit.html", webhookEditData{
				Title: "Edit webhook", ID: wh.ID, Error: perr.Error(),
				Form: webhookFormData{
					URL: url, EntityTypes: domain.EntityTypes, Events: allWebhookEvents,
					CheckedTypes: checkedSet(types), CheckedEvents: checkedSet(events),
				},
			})
			return
		}
		wh.URL, wh.EntityTypes, wh.Events = url, types, events // secret, enabled, created_at preserved
		if err := repo.Update(wh); err != nil {
			notFoundOrServerError(w, r, "webhook", err)
			return
		}
		http.Redirect(w, r, "/webhooks", http.StatusSeeOther)
	}
}

func toggleWebhook(repo *store.WebhookRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := webhookIDParam(w, r)
		if !ok {
			return
		}
		wh, err := repo.Get(id)
		if err != nil {
			notFoundOrServerError(w, r, "webhook", err)
			return
		}
		wh.Enabled = !wh.Enabled
		if err := repo.Update(wh); err != nil {
			notFoundOrServerError(w, r, "webhook", err)
			return
		}
		http.Redirect(w, r, "/webhooks", http.StatusSeeOther)
	}
}

func deleteWebhook(repo *store.WebhookRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := webhookIDParam(w, r)
		if !ok {
			return
		}
		if err := repo.Delete(id); err != nil && !errors.Is(err, store.ErrNotFound) {
			serverError(w, r, err)
			return
		}
		http.Redirect(w, r, "/webhooks", http.StatusSeeOther)
	}
}

// parseWebhookForm reads and validates the create/edit form fields.
func parseWebhookForm(r *http.Request) (url string, types, events []string, err error) {
	if perr := r.ParseForm(); perr != nil {
		return "", nil, nil, errors.New("invalid form submission")
	}
	url = strings.TrimSpace(r.FormValue("url"))
	if url == "" {
		return "", nil, nil, errors.New("URL is required")
	}
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return "", nil, nil, errors.New("URL must start with http:// or https://")
	}
	types = filterAllowed(r.Form["entity_types"], domain.EntityTypes)
	events = filterAllowed(r.Form["events"], allWebhookEvents)
	return url, types, events, nil
}

// filterAllowed keeps only values present in allowed, de-duplicated, preserving
// order — so a hand-crafted POST cannot store bogus filter values.
func filterAllowed(values, allowed []string) []string {
	allow := make(map[string]bool, len(allowed))
	for _, a := range allowed {
		allow[a] = true
	}
	out := []string{}
	seen := map[string]bool{}
	for _, v := range values {
		if allow[v] && !seen[v] {
			out = append(out, v)
			seen[v] = true
		}
	}
	return out
}

func webhookIDParam(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return 0, false
	}
	return id, true
}

// newWebhookSecret returns a random signing secret. Unlike API tokens (stored
// hashed), a webhook secret is stored in plaintext because the delivery signer
// needs it to compute the HMAC — so it is shown once and treated as sensitive.
func newWebhookSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "whsec_" + base64.RawURLEncoding.EncodeToString(b), nil
}
