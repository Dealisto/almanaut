package store

import (
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
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

func mustCreateRel(t *testing.T, repo *RelationshipRepo, ft string, fid int64, tt string, tid int64, kind string) {
	t.Helper()
	if _, err := repo.Create(domain.Relationship{FromType: ft, FromID: fid, ToType: tt, ToID: tid, Kind: kind}); err != nil {
		t.Fatalf("Create rel: %v", err)
	}
}
