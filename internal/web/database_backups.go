package web

import (
	"context"
	"net/http"

	"github.com/Dealisto/almanaut/internal/backup"
)

type databaseBackupLister interface {
	List(context.Context) ([]backup.Manifest, error)
}

type databaseBackupsData struct {
	Title               string
	Enabled, CanRun     bool
	Snapshots           []backup.Manifest
	AdditionalSnapshots int
}

func databaseBackupsPage(snapshots databaseBackupLister, runner jobRunner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := databaseBackupsData{Title: "Database backups", Enabled: snapshots != nil, CanRun: snapshots != nil && runner != nil}
		if snapshots != nil {
			var err error
			data.Snapshots, err = snapshots.List(r.Context())
			if err != nil {
				serverError(w, r, err)
				return
			}
			if len(data.Snapshots) > 50 {
				data.AdditionalSnapshots = len(data.Snapshots) - 50
				data.Snapshots = data.Snapshots[:50]
			}
		}
		render(w, r, "database_backups.html", data)
	}
}
