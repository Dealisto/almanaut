package web

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
	"github.com/go-chi/chi/v5"
)

type tokensPageData struct {
	Title       string
	Tokens      []tokenView
	Scopes      []domain.Scope
	Error       string
	NewToken    string // raw token, shown once immediately after creation
	ActiveCount int
}

type tokenView struct {
	store.APIToken
	ReplacementID int64
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
	active := 0
	replacements := make(map[int64]int64)
	for _, token := range list {
		if !token.Expired() {
			active++
		}
		if token.RotatedFromID != 0 {
			replacements[token.RotatedFromID] = token.ID
		}
	}
	views := make([]tokenView, 0, len(list))
	for _, token := range list {
		views = append(views, tokenView{APIToken: token, ReplacementID: replacements[token.ID]})
	}
	render(w, r, "tokens.html", tokensPageData{Title: "API tokens", Tokens: views, Scopes: domain.Scopes, Error: errMsg, NewToken: newToken, ActiveCount: active})
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
		expires, err := tokenExpiry(r.FormValue("expiry_days"), time.Now().UTC())
		if err != nil {
			renderTokens(w, r, tokens, err.Error(), "")
			return
		}
		raw, err := newAPIToken()
		if err != nil {
			serverError(w, r, err)
			return
		}
		if _, err := tokens.Create(store.APIToken{
			TokenHash: hashToken(raw), UserID: u.ID, Label: label, Scope: scope, CreatedAt: nowRFC3339(), ExpiresAt: expires,
		}); err != nil {
			serverError(w, r, err)
			return
		}
		renderTokens(w, r, tokens, "", raw)
	}
}

func tokenExpiry(raw string, now time.Time) (string, error) {
	switch raw {
	case "", "0":
		return "", nil
	case "7", "30", "90", "365":
		days, _ := strconv.Atoi(raw)
		return now.AddDate(0, 0, days).UTC().Format(time.RFC3339), nil
	default:
		return "", errors.New("invalid token lifetime")
	}
}

// rotateToken prepares a replacement without revoking the credential currently
// deployed. The original owner, scope and label are read within the transaction.
func rotateToken(db *sql.DB, tokens *store.TokenRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := userFrom(r.Context())
		if !ok {
			serverError(w, r, errors.New("no authenticated user in context"))
			return
		}
		id, ok := userIDParam(w, r)
		if !ok {
			return
		}
		expires, err := tokenExpiry(r.FormValue("expiry_days"), time.Now().UTC())
		if err != nil {
			renderTokens(w, r, tokens, err.Error(), "")
			return
		}
		raw, err := newAPIToken()
		if err != nil {
			serverError(w, r, err)
			return
		}
		err = store.WithTx(db, func(tx *sql.Tx) error {
			repo := tokens.WithTx(tx)
			old, err := repo.GetByOwner(id, u.ID)
			if err != nil {
				return err
			}
			if old.RotatedFromID != 0 {
				return &store.ConstraintError{Reason: "finish the current rotation before replacing this token"}
			}
			list, err := repo.ListByUser(u.ID)
			if err != nil {
				return err
			}
			for _, token := range list {
				if token.RotatedFromID == old.ID {
					return &store.ConstraintError{Reason: "a replacement already exists; finish its rotation or revoke it before preparing another"}
				}
			}
			_, err = repo.Create(store.APIToken{
				TokenHash: hashToken(raw), UserID: u.ID, Label: old.Label, Scope: old.Scope,
				CreatedAt: nowRFC3339(), ExpiresAt: expires, RotatedFromID: old.ID,
			})
			return err
		})
		if err != nil {
			if c := constraintError(err); c != nil {
				w.WriteHeader(http.StatusConflict)
				renderTokens(w, r, tokens, c.Reason, "")
			} else {
				notFoundOrServerError(w, r, "token", err)
			}
			return
		}
		renderTokens(w, r, tokens, "", raw)
	}
}

func finishTokenRotation(db *sql.DB, tokens *store.TokenRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := userFrom(r.Context())
		if !ok {
			serverError(w, r, errors.New("no authenticated user in context"))
			return
		}
		id, ok := userIDParam(w, r)
		if !ok {
			return
		}
		err := store.WithTx(db, func(tx *sql.Tx) error {
			repo := tokens.WithTx(tx)
			replacement, err := repo.GetByOwner(id, u.ID)
			if err != nil {
				return err
			}
			if replacement.RotatedFromID == 0 {
				return &store.ConstraintError{Reason: "this token has no pending rotation"}
			}
			if replacement.LastUsedAt == "" || replacement.Expired() {
				return &store.ConstraintError{Reason: "use the unexpired replacement token before retiring the original"}
			}
			if err := repo.Delete(replacement.RotatedFromID, u.ID); err != nil {
				return fmt.Errorf("retire original token: %w", err)
			}
			return nil
		})
		if err != nil {
			if c := constraintError(err); c != nil {
				w.WriteHeader(http.StatusConflict)
				renderTokens(w, r, tokens, c.Reason, "")
			} else {
				notFoundOrServerError(w, r, "token", err)
			}
			return
		}
		http.Redirect(w, r, "/account/tokens", http.StatusSeeOther)
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
