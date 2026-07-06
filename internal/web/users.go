package web

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
	"github.com/go-chi/chi/v5"
)

// errLastUser signals an attempt to delete the only remaining account; the
// guard runs inside a transaction so a concurrent delete cannot race past it.
var errLastUser = errors.New("cannot delete the last remaining user")

type usersPageData struct {
	Title string
	Users []domain.User
	Error string
}

type passwordPageData struct {
	Title   string
	Error   string
	Success string
}

// renderUsers lists all users, optionally with a form error.
func renderUsers(w http.ResponseWriter, r *http.Request, users *store.UserRepo, errMsg string) {
	list, err := users.List()
	if err != nil {
		serverError(w, r, err)
		return
	}
	render(w, r, "users.html", usersPageData{Title: "Users", Users: list, Error: errMsg})
}

func listUsers(users *store.UserRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		renderUsers(w, r, users, "")
	}
}

func createUser(users *store.UserRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username := strings.TrimSpace(r.FormValue("username"))
		password := r.FormValue("password")
		u := domain.User{Username: username}
		if err := u.Validate(); err != nil {
			renderUsers(w, r, users, err.Error())
			return
		}
		if err := domain.ValidatePassword(password); err != nil {
			renderUsers(w, r, users, err.Error())
			return
		}
		if _, err := users.GetByUsername(username); err == nil {
			renderUsers(w, r, users, "a user with that name already exists")
			return
		} else if !errors.Is(err, store.ErrNotFound) {
			serverError(w, r, err)
			return
		}
		hash, err := hashPassword(password)
		if err != nil {
			serverError(w, r, err)
			return
		}
		now := nowRFC3339()
		if _, err := users.Create(domain.User{Username: username, PasswordHash: hash, CreatedAt: now, UpdatedAt: now}); err != nil {
			serverError(w, r, err)
			return
		}
		http.Redirect(w, r, "/users", http.StatusSeeOther)
	}
}

func deleteUser(users *store.UserRepo, db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := userIDParam(w, r)
		if !ok {
			return
		}
		// Guard against locking everyone out: never delete the last account.
		// The count-check-and-delete runs inside a transaction so two
		// concurrent deletes can't both observe count > 1 and both proceed.
		err := store.WithTx(db, func(tx *sql.Tx) error {
			ur := users.WithTx(tx)
			n, err := ur.Count()
			if err != nil {
				return err
			}
			if n <= 1 {
				return errLastUser
			}
			return ur.Delete(id)
		})
		if errors.Is(err, errLastUser) {
			renderUsers(w, r, users, "cannot delete the last remaining user")
			return
		}
		if err != nil {
			serverError(w, r, err)
			return
		}
		http.Redirect(w, r, "/users", http.StatusSeeOther)
	}
}

func resetUserPassword(users *store.UserRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := userIDParam(w, r)
		if !ok {
			return
		}
		password := r.FormValue("password")
		if err := domain.ValidatePassword(password); err != nil {
			renderUsers(w, r, users, err.Error())
			return
		}
		hash, err := hashPassword(password)
		if err != nil {
			serverError(w, r, err)
			return
		}
		if err := users.UpdatePassword(id, hash, nowRFC3339()); err != nil {
			notFoundOrServerError(w, r, "user", err)
			return
		}
		http.Redirect(w, r, "/users", http.StatusSeeOther)
	}
}

func changePasswordForm(w http.ResponseWriter, r *http.Request) {
	render(w, r, "password.html", passwordPageData{Title: "Change password"})
}

func changePassword(users *store.UserRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := userFrom(r.Context())
		if !ok {
			serverError(w, r, errors.New("no authenticated user in context"))
			return
		}
		current := r.FormValue("current_password")
		next := r.FormValue("new_password")
		if !verifyPassword(u.PasswordHash, current) {
			render(w, r, "password.html", passwordPageData{Title: "Change password", Error: "current password is incorrect"})
			return
		}
		if err := domain.ValidatePassword(next); err != nil {
			render(w, r, "password.html", passwordPageData{Title: "Change password", Error: err.Error()})
			return
		}
		hash, err := hashPassword(next)
		if err != nil {
			serverError(w, r, err)
			return
		}
		if err := users.UpdatePassword(u.ID, hash, nowRFC3339()); err != nil {
			serverError(w, r, err)
			return
		}
		render(w, r, "password.html", passwordPageData{Title: "Change password", Success: "password updated"})
	}
}

// userIDParam parses the {id} URL param, writing a 400 on a malformed value.
func userIDParam(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return 0, false
	}
	return id, true
}
