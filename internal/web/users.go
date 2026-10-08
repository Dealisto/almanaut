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

// errLastAdmin signals an attempt to demote or delete the only admin, which
// would leave nobody able to manage users (ALMANAUT_RESET_ADMIN restores one).
var errLastAdmin = errors.New("cannot remove the last admin")

type usersPageData struct {
	Title   string
	Users   []domain.User
	Roles   []domain.Role
	NewRole domain.Role // default selection for the create-user form (least privilege)
	Error   string
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
	render(w, r, "users.html", usersPageData{Title: "Users", Users: list, Roles: domain.Roles, NewRole: domain.RoleViewer, Error: errMsg})
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
		role := domain.Role(strings.TrimSpace(r.FormValue("role")))
		if role == "" {
			role = domain.RoleViewer
		}
		u := domain.User{Username: username, Role: role}
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
		if _, err := users.Create(domain.User{Username: username, Role: role, PasswordHash: hash, CreatedAt: now, UpdatedAt: now}); err != nil {
			serverError(w, r, err)
			return
		}
		http.Redirect(w, r, "/users", http.StatusSeeOther)
	}
}

func updateUserRole(users *store.UserRepo, db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := userIDParam(w, r)
		if !ok {
			return
		}
		role := domain.Role(strings.TrimSpace(r.FormValue("role")))
		if !role.Valid() {
			renderUsers(w, r, users, "invalid role")
			return
		}
		// Demoting the last admin would leave nobody able to manage users. The
		// count-check-and-update runs in one transaction so two concurrent
		// demotions cannot both see a second admin and both proceed.
		err := store.WithTx(db, func(tx *sql.Tx) error {
			ur := users.WithTx(tx)
			target, err := ur.Get(id)
			if err != nil {
				return err
			}
			if target.Role == domain.RoleAdmin && role != domain.RoleAdmin {
				if err := requireAnotherAdmin(ur); err != nil {
					return err
				}
			}
			return ur.UpdateRole(id, role, nowRFC3339())
		})
		if errors.Is(err, errLastAdmin) {
			renderUsers(w, r, users, errLastAdmin.Error())
			return
		}
		if err != nil {
			notFoundOrServerError(w, r, "user", err)
			return
		}
		http.Redirect(w, r, "/users", http.StatusSeeOther)
	}
}

// requireAnotherAdmin returns errLastAdmin unless more than one admin exists.
// Call it on a tx-bound repo, inside the transaction that removes an admin.
func requireAnotherAdmin(ur *store.UserRepo) error {
	n, err := ur.CountByRole(domain.RoleAdmin)
	if err != nil {
		return err
	}
	if n <= 1 {
		return errLastAdmin
	}
	return nil
}

func deleteUser(users *store.UserRepo, db *sql.DB, audit *store.AuthEventRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := userIDParam(w, r)
		if !ok {
			return
		}
		// Guard against locking everyone out: never delete the last account or
		// the last admin. The checks and the delete run inside one transaction
		// so two concurrent deletes can't both pass them.
		var target domain.User
		err := store.WithTx(db, func(tx *sql.Tx) error {
			ur := users.WithTx(tx)
			var err error
			// Read the target first: the audit event names the deleted user.
			if target, err = ur.Get(id); err != nil {
				return err
			}
			n, err := ur.Count()
			if err != nil {
				return err
			}
			if n <= 1 {
				return errLastUser
			}
			if target.Role == domain.RoleAdmin {
				if err := requireAnotherAdmin(ur); err != nil {
					return err
				}
			}
			return ur.Delete(id)
		})
		if errors.Is(err, errLastUser) || errors.Is(err, errLastAdmin) {
			renderUsers(w, r, users, err.Error())
			return
		}
		if err != nil {
			notFoundOrServerError(w, r, "user", err)
			return
		}
		recordAuth(audit, r, domain.AuthSessionRevoked, target.Username, id, "user deleted by "+actor(r))
		http.Redirect(w, r, "/users", http.StatusSeeOther)
	}
}

// revokeSessions ends every session userID holds; call it on a tx-bound repo
// in the same transaction as the credential change that warrants it. When the
// request is itself one of that user's sessions, that session is kept, so
// acting on your own account does not sign you out of the page you are using.
func revokeSessions(sessions *store.SessionRepo, r *http.Request, userID int64) error {
	if u, ok := userFrom(r.Context()); ok && u.ID == userID {
		if c, err := r.Cookie(sessionCookieName); err == nil && c.Value != "" {
			return sessions.DeleteOtherSessions(userID, hashToken(c.Value))
		}
	}
	return sessions.DeleteByUser(userID)
}

// setPasswordAndRevoke stores a new password hash for userID and ends their
// other sessions in one transaction: a session stolen before the change must
// not outlive it.
func setPasswordAndRevoke(db *sql.DB, users *store.UserRepo, sessions *store.SessionRepo, r *http.Request, userID int64, hash string) error {
	return store.WithTx(db, func(tx *sql.Tx) error {
		if err := users.WithTx(tx).UpdatePassword(userID, hash, nowRFC3339()); err != nil {
			return err
		}
		return revokeSessions(sessions.WithTx(tx), r, userID)
	})
}

func resetUserPassword(users *store.UserRepo, sessions *store.SessionRepo, db *sql.DB, audit *store.AuthEventRepo) http.HandlerFunc {
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
		if err := setPasswordAndRevoke(db, users, sessions, r, id, hash); err != nil {
			notFoundOrServerError(w, r, "user", err)
			return
		}
		if target, err := users.Get(id); err == nil {
			recordAuth(audit, r, domain.AuthSessionRevoked, target.Username, id, "password reset by "+actor(r))
		}
		http.Redirect(w, r, "/users", http.StatusSeeOther)
	}
}

func changePasswordForm(w http.ResponseWriter, r *http.Request) {
	render(w, r, "password.html", passwordPageData{Title: "Change password"})
}

func changePassword(users *store.UserRepo, sessions *store.SessionRepo, db *sql.DB, audit *store.AuthEventRepo) http.HandlerFunc {
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
		if err := setPasswordAndRevoke(db, users, sessions, r, u.ID, hash); err != nil {
			serverError(w, r, err)
			return
		}
		recordAuth(audit, r, domain.AuthSessionRevoked, u.Username, u.ID, "password changed")
		render(w, r, "password.html", passwordPageData{Title: "Change password", Success: "password updated; your other sessions were signed out"})
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
