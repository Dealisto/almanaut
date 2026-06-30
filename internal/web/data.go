package web

import (
	"database/sql"
	"io"
	"net/http"

	"github.com/Dealisto/almanaut/internal/store"
	"gopkg.in/yaml.v3"
)

type dataPageData struct {
	Title    string
	Error    string
	Imported bool
}

func showData() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		render(w, req, "data.html", dataPageData{
			Title:    "Data",
			Imported: req.URL.Query().Get("imported") == "1",
		})
	}
}

func exportData(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		snap, err := store.Export(db)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out, err := yaml.Marshal(snap)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
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
			http.Error(w, err.Error(), http.StatusInternalServerError)
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
