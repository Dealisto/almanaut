package web

import (
	"database/sql"
	"net/http"
	"strings"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
	"github.com/Dealisto/almanaut/internal/webhook"
	"github.com/go-chi/chi/v5"
)

type hostTypesPageData struct {
	Title                                        string
	Types                                        []domain.HostType
	Error                                        string
	Name, Label                                  string
	EditName, EditLabel, DeleteName, Replacement string
}

func renderHostTypes(w http.ResponseWriter, req *http.Request, repo *store.HostTypeRepo, cause error) {
	types, err := repo.List()
	if err != nil {
		serverError(w, req, err)
		return
	}
	data := hostTypesPageData{Title: "Host types", Types: types}
	if cause != nil {
		data.Error = cause.Error()
		name := chi.URLParam(req, "name")
		if name == "" {
			data.Name, data.Label = req.FormValue("name"), req.FormValue("label")
		} else if strings.HasSuffix(req.URL.Path, "/delete") {
			data.DeleteName, data.Replacement = name, req.FormValue("replacement")
		} else {
			data.EditName, data.EditLabel = name, req.FormValue("label")
		}
	}
	render(w, req, "host_types.html", data)
}

func hostTypesPage(repo *store.HostTypeRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) { renderHostTypes(w, req, repo, nil) }
}

func hostTypesJSON(repo *store.HostTypeRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		types, err := repo.List()
		if err != nil {
			apiServerError(w, req, err)
			return
		}
		writeJSON(w, http.StatusOK, types)
	}
}

func hostTypeResult(w http.ResponseWriter, req *http.Request, repo *store.HostTypeRepo, err error) {
	if err == nil {
		http.Redirect(w, req, "/host-types", http.StatusSeeOther)
		return
	}
	if c := constraintError(err); c != nil {
		renderHostTypes(w, req, repo, c)
		return
	}
	notFoundOrServerError(w, req, "host type", err)
}

func createHostType(repo *store.HostTypeRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		name := strings.TrimSpace(req.FormValue("name"))
		if name == "" {
			name = domain.SlugifyCustomField(req.FormValue("label"))
		}
		err := repo.Create(domain.HostType{Name: name, Label: req.FormValue("label")})
		hostTypeResult(w, req, repo, err)
	}
}

func updateHostType(repo *store.HostTypeRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		err := repo.UpdateLabel(chi.URLParam(req, "name"), req.FormValue("label"))
		hostTypeResult(w, req, repo, err)
	}
}

func deleteHostType(repo *store.HostTypeRepo, hosts *store.HostRepo, rs resource[domain.Host], d handlerDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		name, replacement := chi.URLParam(req, "name"), req.FormValue("replacement")
		var events []webhook.Event
		err := store.WithTx(d.db, func(tx *sql.Tx) error {
			types := repo.WithTx(tx)
			t, err := types.Get(name)
			if err != nil {
				return err
			}
			if t.Builtin() {
				return &store.ConstraintError{Reason: "built-in host types cannot be deleted"}
			}
			if replacement != "" {
				if replacement == name {
					return &store.ConstraintError{Reason: "choose a different replacement type"}
				}
				if err := types.Require(replacement); err != nil {
					return err
				}
				items, err := hosts.WithTx(tx).List()
				if err != nil {
					return err
				}
				for _, h := range items {
					if h.Type != name {
						continue
					}
					h.Type = replacement
					// Use the normal host update path for history, custom fields and webhooks.
					if err := rs.updateEntityTx(tx, d, h, nil, actor(req), &events); err != nil {
						return err
					}
				}
			}
			return types.Delete(name)
		})
		if err == nil {
			d.webhooks.Dispatch(events...)
		}
		hostTypeResult(w, req, repo, err)
	}
}
