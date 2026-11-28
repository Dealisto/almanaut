package domain

import (
	"testing"
	"time"
)

func TestBackupRunHealthRequiresReportedSuccess(t *testing.T) {
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	hours := int64(24)
	backup := Backup{Source: "external", LastRun: "2026-11-27", ExpectedIntervalHours: &hours}
	if state, _ := BackupRunHealth(backup, nil, now); state != "never" {
		t.Fatal(state)
	}
	for _, tc := range []struct {
		status string
		age    time.Duration
		want   string
	}{{"success", time.Hour, "healthy"}, {"success", 25 * time.Hour, "stale"}, {"failed", time.Hour, "failed"}, {"failed", 25 * time.Hour, "failed"}} {
		run := BackupRun{Status: tc.status, CompletedAt: now.Add(-tc.age).Format(time.RFC3339)}
		if state, _ := BackupRunHealth(backup, &run, now); state != tc.want {
			t.Errorf("%+v state=%s", tc, state)
		}
	}
	backup.ExpectedIntervalHours = nil
	if state, _ := BackupRunHealth(backup, nil, now); state != "unmonitored" {
		t.Fatal(state)
	}
}

func TestBackupRunValidation(t *testing.T) {
	now := time.Now().UTC()
	negative := int64(-1)
	valid := BackupRun{RunID: "run", Status: "success", CompletedAt: now.Format(time.RFC3339Nano)}
	for _, modify := range []func(*BackupRun){func(r *BackupRun) { r.RunID = "" }, func(r *BackupRun) { r.Status = "running" }, func(r *BackupRun) { r.CompletedAt = "today" }, func(r *BackupRun) { r.CompletedAt = now.Add(time.Hour).Format(time.RFC3339) }, func(r *BackupRun) { r.StartedAt = now.Add(time.Hour).Format(time.RFC3339) }, func(r *BackupRun) { r.SizeBytes = &negative }, func(r *BackupRun) { r.DurationSeconds = &negative }} {
		run := valid
		modify(&run)
		if _, err := NormalizeBackupRun(run, now); err == nil {
			t.Fatalf("invalid report accepted %+v", run)
		}
	}
	if _, err := NormalizeBackupRun(valid, now); err != nil {
		t.Fatal(err)
	}
}
