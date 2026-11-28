package web

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
	"github.com/Dealisto/almanaut/internal/webhook"
	"github.com/go-chi/chi/v5"
)

func parseBackupExpectedInterval(get func(string) string) *int64 {
	raw := strings.TrimSpace(get("expected_interval_hours"))
	if raw == "" && get("backup_policy_present") != "1" {
		return nil
	}
	value, _ := strconv.ParseInt(raw, 10, 64) // parseInput validates malformed scalars.
	return &value
}

func backupRunsChildren(b domain.Backup, _ entityCatalog) (*childrenSection, error) {
	return &childrenSection{Title: "External backup job", Items: []childRef{{Label: "Reported runs and job health", Path: fmt.Sprintf("/backups/%d/runs", b.ID)}}}, nil
}

type backupRunsPageData struct {
	Title   string
	Backup  domain.Backup
	Runs    []domain.BackupRun
	State   string
	Message string
}

func backupRunsPage(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil || id <= 0 {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		backup, err := store.NewBackupRepo(db).Get(id)
		if err != nil {
			notFoundOrServerError(w, req, "backup", err)
			return
		}
		runs, err := store.NewBackupRunRepo(db).List(req.Context(), id)
		if err != nil {
			serverError(w, req, err)
			return
		}
		var latest *domain.BackupRun
		if len(runs) > 0 {
			latest = &runs[0]
		}
		state, message := domain.BackupRunHealth(backup, latest, time.Now())
		render(w, req, "backup_runs.html", backupRunsPageData{Title: "Backup job runs", Backup: backup, Runs: runs, State: state, Message: message})
	}
}

func backupRunsJSON(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil || id <= 0 {
			writeJSONError(w, http.StatusBadRequest, "invalid id")
			return
		}
		if _, err := store.NewBackupRepo(db).Get(id); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeJSONError(w, http.StatusNotFound, "backup not found")
			} else {
				apiServerError(w, req, err)
			}
			return
		}
		runs, err := store.NewBackupRunRepo(db).List(req.Context(), id)
		if err != nil {
			apiServerError(w, req, err)
			return
		}
		writeJSON(w, http.StatusOK, runs)
	}
}

func reportBackupRun(deps handlerDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil || id <= 0 {
			writeJSONError(w, http.StatusBadRequest, "invalid id")
			return
		}
		// Reports carry summaries, never backup payloads. Bound them independently
		// of the larger inventory upload limit.
		req.Body = http.MaxBytesReader(w, req.Body, 16<<10)
		raw, fresh, ok := readAPIBody(w, req, "invalid backup run report", domain.ScopeReadWrite)
		if !ok {
			return
		}
		req = fresh
		var input struct {
			RunID           string `json:"run_id"`
			Status          string `json:"status"`
			StartedAt       string `json:"started_at"`
			CompletedAt     string `json:"completed_at"`
			DurationSeconds *int64 `json:"duration_seconds"`
			SizeBytes       *int64 `json:"size_bytes"`
			Message         string `json:"message"`
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid backup run JSON: "+err.Error())
			return
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			writeJSONError(w, http.StatusBadRequest, "exactly one JSON report is required")
			return
		}
		run := domain.BackupRun{BackupID: id, RunID: input.RunID, Status: input.Status, StartedAt: input.StartedAt, CompletedAt: input.CompletedAt, DurationSeconds: input.DurationSeconds, SizeBytes: input.SizeBytes, Message: input.Message}
		now := time.Now()
		run, err = domain.NormalizeBackupRun(run, now)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		created := false
		var events []webhook.Event
		err = store.WithTxContext(req.Context(), deps.db, func(tx *sql.Tx) error {
			var err error
			run, created, err = store.NewBackupRunRepo(deps.db).RecordTx(tx, run, now)
			if err != nil || !created {
				return err
			}
			backup, err := store.NewBackupRepo(deps.db).GetTx(tx, id)
			if err != nil {
				return err
			}
			who := actor(req)
			timestamp := now.UTC().Format(time.RFC3339)
			if err := deps.changelog.WithTx(tx).Create(store.ChangeEvent{EntityType: "backup", EntityID: id, Label: backup.Source, Action: domain.ActionUpdate, Actor: who, CreatedAt: timestamp, Changes: []domain.FieldChange{{Field: "reported_run", New: run.RunID}, {Field: "reported_status", New: run.Status}, {Field: "completed_at", New: run.CompletedAt}}}); err != nil {
				return err
			}
			event, err := webhook.NewEvent("backup", id, webhook.ActionUpdated, who, timestamp, struct {
				domain.Backup
				Run domain.BackupRun `json:"reported_run"`
			}{backup, run})
			if err != nil {
				return err
			}
			if err := webhook.Stage(tx, webhook.ForTransaction(tx, deps.webhooks), event); err != nil {
				return err
			}
			events = append(events, event)
			return nil
		})
		if err != nil {
			switch {
			case errors.Is(err, store.ErrBackupRunConflict), errors.Is(err, store.ErrBackupRunTooOld):
				writeJSONError(w, http.StatusConflict, err.Error())
			case errors.Is(err, store.ErrNotFound):
				writeJSONError(w, http.StatusNotFound, "backup not found")
			default:
				if invalid := constraintError(err); invalid != nil {
					writeJSONError(w, http.StatusBadRequest, invalid.Error())
				} else {
					apiServerError(w, req, err)
				}
			}
			return
		}
		deps.webhooks.Dispatch(events...)
		status := http.StatusOK
		if created {
			status = http.StatusCreated
		}
		writeJSON(w, status, run)
	}
}

func restoreBackupPolicyDraft(current domain.Backup, draft *domain.Backup) {
	if draft.ExpectedIntervalHours == nil && current.ExpectedIntervalHours != nil {
		value := *current.ExpectedIntervalHours
		draft.ExpectedIntervalHours = &value
	}
}
