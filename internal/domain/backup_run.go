package domain

import (
	"fmt"
	"strings"
	"time"
)

// BackupRun is one completed run reported by an external backup job. It is
// operational evidence, separate from the manually documented LastRun date.
type BackupRun struct {
	ID              int64  `json:"id"`
	BackupID        int64  `json:"backup_id"`
	RunID           string `json:"run_id"`
	Status          string `json:"status"`
	StartedAt       string `json:"started_at"`
	CompletedAt     string `json:"completed_at"`
	DurationSeconds *int64 `json:"duration_seconds"`
	SizeBytes       *int64 `json:"size_bytes"`
	Message         string `json:"message"`
	ReceivedAt      string `json:"received_at"`
}

const backupRunTimeFormat = "2006-01-02T15:04:05.000000000Z"

// NormalizeBackupRun validates caller-controlled evidence and uses fixed-width
// UTC timestamps so chronological database ordering is exact.
func NormalizeBackupRun(run BackupRun, now time.Time) (BackupRun, error) {
	run.RunID = strings.TrimSpace(run.RunID)
	if run.RunID == "" || len(run.RunID) > 200 || strings.ContainsAny(run.RunID, "\r\n\x00") {
		return run, fmt.Errorf("run_id must contain 1–200 bytes without line breaks")
	}
	if run.Status != "success" && run.Status != "failed" {
		return run, fmt.Errorf("status must be success or failed")
	}
	completed, err := time.Parse(time.RFC3339Nano, run.CompletedAt)
	if err != nil {
		return run, fmt.Errorf("completed_at must be an RFC3339 timestamp")
	}
	if completed.After(now.Add(5 * time.Minute)) {
		return run, fmt.Errorf("completed_at cannot be more than five minutes in the future")
	}
	if run.StartedAt != "" {
		started, err := time.Parse(time.RFC3339Nano, run.StartedAt)
		if err != nil || started.After(completed) {
			return run, fmt.Errorf("started_at must be an RFC3339 timestamp no later than completed_at")
		}
		run.StartedAt = started.UTC().Format(backupRunTimeFormat)
	}
	if run.DurationSeconds != nil && (*run.DurationSeconds < 0 || *run.DurationSeconds > 315576000) {
		return run, fmt.Errorf("duration_seconds must be between 0 and 315576000")
	}
	if run.SizeBytes != nil && *run.SizeBytes < 0 {
		return run, fmt.Errorf("size_bytes must not be negative")
	}
	if len(run.Message) > 4096 {
		return run, fmt.Errorf("message must not exceed 4096 bytes")
	}
	run.CompletedAt = completed.UTC().Format(backupRunTimeFormat)
	return run, nil
}

// BackupRunHealth never treats a documented date as a verified successful run.
func BackupRunHealth(backup Backup, latest *BackupRun, now time.Time) (string, string) {
	if backup.ExpectedIntervalHours == nil || *backup.ExpectedIntervalHours == 0 {
		return "unmonitored", "Set an expected interval to monitor external job reports."
	}
	if latest == nil {
		return "never", "No completed backup run has been reported."
	}
	if latest.Status == "failed" {
		return "failed", "The latest reported backup run failed."
	}
	completed, err := time.Parse(time.RFC3339Nano, latest.CompletedAt)
	if err != nil || now.Sub(completed) > time.Duration(*backup.ExpectedIntervalHours)*time.Hour {
		return "stale", "The latest successful backup run is older than its expected interval."
	}
	return "healthy", "A successful backup run was reported within its expected interval."
}
