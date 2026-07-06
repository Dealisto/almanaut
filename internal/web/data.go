package web

import (
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/Dealisto/almanaut/internal/store"
	"gopkg.in/yaml.v3"
)

type dataPageData struct {
	Title       string
	Error       string
	Imported    bool
	Types       []importType // CSV import type picker
	CSVType     string       // repopulate the picker after a row-error re-render
	CSVErrors   []string     // per-row import errors (import aborted)
	CSVImported bool
	CSVCreated  int
	CSVUpdated  int
}

func showData(cat entityCatalog) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		q := req.URL.Query()
		data := dataPageData{
			Title:    "Data",
			Imported: q.Get("imported") == "1",
			Types:    cat.importTypes(),
		}
		if q.Get("csv_imported") == "1" {
			data.CSVImported = true
			data.CSVCreated, _ = strconv.Atoi(q.Get("created"))
			data.CSVUpdated, _ = strconv.Atoi(q.Get("updated"))
		}
		render(w, req, "data.html", data)
	}
}

func exportData(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		snap, err := store.Export(db)
		if err != nil {
			serverError(w, req, err)
			return
		}
		out, err := yaml.Marshal(snap)
		if err != nil {
			serverError(w, req, err)
			return
		}
		w.Header().Set("Content-Type", "application/x-yaml; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="almanaut-export.yaml"`)
		_, _ = w.Write(out)
	}
}

func importData(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if err := req.ParseMultipartForm(10 << 20); err != nil {
			http.Error(w, "could not parse upload", http.StatusBadRequest)
			return
		}
		defer func() {
			if req.MultipartForm != nil {
				_ = req.MultipartForm.RemoveAll()
			}
		}()
		if req.FormValue("confirm") == "" {
			http.Error(w, "you must confirm that all data will be replaced", http.StatusBadRequest)
			return
		}
		file, _, err := req.FormFile("file")
		if err != nil {
			http.Error(w, "missing import file", http.StatusBadRequest)
			return
		}
		defer file.Close()
		raw, err := io.ReadAll(file)
		if err != nil {
			serverError(w, req, err)
			return
		}
		var snap store.Snapshot
		if err := yaml.Unmarshal(raw, &snap); err != nil {
			render(w, req, "data.html", dataPageData{Title: "Data", Error: "invalid YAML: " + err.Error()})
			return
		}
		if err := store.Import(db, snap); err != nil {
			render(w, req, "data.html", dataPageData{Title: "Data", Error: err.Error()})
			return
		}
		http.Redirect(w, req, "/data?imported=1", http.StatusSeeOther)
	}
}

func importCSV(cat entityCatalog, deps handlerDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if err := req.ParseMultipartForm(10 << 20); err != nil {
			http.Error(w, "could not parse upload", http.StatusBadRequest)
			return
		}
		defer func() {
			if req.MultipartForm != nil {
				_ = req.MultipartForm.RemoveAll()
			}
		}()
		typ := req.FormValue("type")
		rs, ok := cat.resource(typ)
		if !ok {
			http.Error(w, "unknown entity type", http.StatusBadRequest)
			return
		}
		if req.FormValue("confirm") == "" {
			http.Error(w, "you must confirm the import", http.StatusBadRequest)
			return
		}
		file, _, err := req.FormFile("file")
		if err != nil {
			http.Error(w, "missing import file", http.StatusBadRequest)
			return
		}
		defer file.Close()

		created, updated, rowErrs, err := rs.importCSV(deps, file, actor(req))
		if err != nil {
			serverError(w, req, err)
			return
		}
		if len(rowErrs) > 0 {
			render(w, req, "data.html", dataPageData{
				Title: "Data", Types: cat.importTypes(), CSVType: typ, CSVErrors: rowErrs,
			})
			return
		}
		http.Redirect(w, req, fmt.Sprintf("/data?csv_imported=1&created=%d&updated=%d", created, updated), http.StatusSeeOther)
	}
}
