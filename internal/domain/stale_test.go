package domain

import (
	"testing"
	"time"
)

func TestStaleRefs(t *testing.T) {
	now := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	refs := []EntityRef{
		{Type: "host", ID: 1},        // fresh
		{Type: "host", ID: 2},        // stale
		{Type: "service", ID: 3},     // no activity -> skipped
		{Type: "certificate", ID: 4}, // unparseable -> skipped
	}
	activity := map[EntityRef]string{
		{Type: "host", ID: 1}:        now.AddDate(0, 0, -10).Format(time.RFC3339),
		{Type: "host", ID: 2}:        now.AddDate(0, 0, -120).Format(time.RFC3339),
		{Type: "certificate", ID: 4}: "not-a-time",
	}
	got := StaleRefs(refs, activity, now, 90)
	if len(got) != 1 || got[0] != (EntityRef{Type: "host", ID: 2}) {
		t.Errorf("StaleRefs = %+v, want [host:2]", got)
	}
}

func TestStaleRefsDisabled(t *testing.T) {
	now := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	refs := []EntityRef{{Type: "host", ID: 1}}
	activity := map[EntityRef]string{{Type: "host", ID: 1}: "2000-01-01T00:00:00Z"}
	if got := StaleRefs(refs, activity, now, 0); got != nil {
		t.Errorf("days<=0 should disable the rule, got %+v", got)
	}
}

func TestStaleRefsBoundary(t *testing.T) {
	now := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	ref := EntityRef{Type: "host", ID: 1}
	// Exactly at the cutoff is not yet stale (strictly older required).
	activity := map[EntityRef]string{ref: now.AddDate(0, 0, -90).Format(time.RFC3339)}
	if got := StaleRefs([]EntityRef{ref}, activity, now, 90); len(got) != 0 {
		t.Errorf("at exactly the window it should not be stale, got %+v", got)
	}
}
