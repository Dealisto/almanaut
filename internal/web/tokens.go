package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
	"github.com/go-chi/chi/v5"
)

type tokensPageData struct {
	Title    string
	Tokens   []store.APIToken
	Scopes   []domain.Scope
	Error    string
	NewToken string // raw token, shown once immediately after creation
}

// renderTokens lists the current user's tokens, optionally with a form error or a
// freshly created raw token to display once.
func renderTokens(w http.ResponseWriter, r *http.Request, tokens *store.TokenRepo, errMsg, newToken string) {
	u, ok := userFrom(r.Context())
	if !ok {
		serverError(w, r, errors.New("no authenticated user in context"))
		return
	}
	list, err := tokens.ListByUser(u.ID)
	if err != nil {
		serverError(w, r, err)
		return
	}
	render(w, r, "tokens.html", tokensPageData{Title: "API tokens", Tokens: list, Scopes: domain.Scopes, Error: errMsg, NewToken: newToken})
}

func listTokens(tokens *store.TokenRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		renderTokens(w, r, tokens, "", "")
	}
}

func createToken(tokens *store.TokenRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := userFrom(r.Context())
		if !ok {
			serverError(w, r, errors.New("no authenticated user in context"))
			return
		}
		label := strings.TrimSpace(r.FormValue("label"))
		if label == "" {
			renderTokens(w, r, tokens, "label is required", "")
			return
		}
		scope := strings.TrimSpace(r.FormValue("scope"))
		if scope == "" {
			scope = string(domain.ScopeReadWrite)
		}
		if !domain.Scope(scope).Valid() {
			renderTokens(w, r, tokens, "invalid token scope", "")
			return
		}
		raw, err := newAPIToken()
		if err != nil {
			serverError(w, r, err)
			return
		}
		if _, err := tokens.Create(store.APIToken{
			TokenHash: hashToken(raw), UserID: u.ID, Label: label, Scope: scope, CreatedAt: nowRFC3339(),
		}); err != nil {
			serverError(w, r, err)
			return
		}
		renderTokens(w, r, tokens, "", raw)
	}
}

func deleteToken(tokens *store.TokenRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := userFrom(r.Context())
		if !ok {
			serverError(w, r, errors.New("no authenticated user in context"))
			return
		}
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		// A token already gone (or not owned) is not an error to the user: either
		// way it no longer grants access. Only a real backend failure is a 500.
		if err := tokens.Delete(id, u.ID); err != nil && !errors.Is(err, store.ErrNotFound) {
			serverError(w, r, err)
			return
		}
		http.Redirect(w, r, "/account/tokens", http.StatusSeeOther)
	}
}
