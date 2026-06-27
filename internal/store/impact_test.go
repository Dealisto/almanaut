package store

import (
	"testing"
)

func TestImpact(t *testing.T) {
	repo := newRelationshipRepo(t)
	// service:1 runs on host:1 ; domain:1 exposed via service:1 ; backup:1 backed up by host:1
	mustCreateRel(t, repo, "service", 1, "host", 1, "runs on")
	mustCreateRel(t, repo, "domain", 1, "service", 1, "exposed via")
	mustCreateRel(t, repo, "backup", 1, "host", 1, "backed up by")

	impacted, err := Impact(repo, "host", 1)
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	got := map[string]bool{}
	for _, r := range impacted {
		got[r.Type] = true
	}
	if !got["service"] || !got["domain"] {
		t.Errorf("expected service and domain in impact, got %+v", impacted)
	}
	if got["backup"] {
		t.Error("backup is linked by a non-dependency kind and must NOT be in the impact set")
	}
	if len(impacted) != 2 {
		t.Errorf("impact size = %d, want 2", len(impacted))
	}
}
