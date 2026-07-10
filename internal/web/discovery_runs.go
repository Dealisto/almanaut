package web

import (
	"net/http"

	"github.com/Dealisto/almanaut/internal/store"
)

type discoveryRunView struct {
	Source     string
	StartedAt  string
	FinishedAt string
	FoundCount int
	NewCount   int
	Error      string
}

type discoveryRunsData struct {
	Title string
	Runs  []discoveryRunView
}

func discoveryRunsPage(runs *store.DiscoveryRunRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		list, err := runs.List(50)
		if err != nil {
			serverError(w, r, err)
			return
		}
		views := make([]discoveryRunView, 0, len(list))
		for _, run := range list {
			views = append(views, discoveryRunView{
				Source:     run.Source,
				StartedAt:  run.StartedAt.Format("2006-01-02 15:04"),
				FinishedAt: run.FinishedAt.Format("15:04:05"),
				FoundCount: run.FoundCount,
				NewCount:   run.NewCount,
				Error:      run.Error,
			})
		}
		render(w, r, "discovery_runs.html", discoveryRunsData{Title: "Discovery runs", Runs: views})
	}
}
