package web

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Dealisto/almanaut/internal/job"
)

// jobRunner is the subset of *job.Runner the admin page needs.
type jobRunner interface {
	Statuses() []job.Status
	Trigger(name string) bool
}

type tasksPageData struct {
	Title   string
	Jobs    []job.Status
	Message string
}

func tasksPage(runner jobRunner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		render(w, r, "tasks.html", tasksPageData{
			Title:   "Scheduled tasks",
			Jobs:    runner.Statuses(),
			Message: r.URL.Query().Get("msg"),
		})
	}
}

func tasksRun(runner jobRunner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// The name comes from a rendered button, not free user input, so an
		// unknown name is a bug — fail soft with a benign message, never a 500.
		msg := "run-requested"
		if !runner.Trigger(chi.URLParam(r, "name")) {
			msg = "unknown-job"
		}
		http.Redirect(w, r, "/tasks?msg="+msg, http.StatusSeeOther)
	}
}
