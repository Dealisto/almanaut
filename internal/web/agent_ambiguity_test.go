package web

import (
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestAgentAmbiguousAdoptionDoesNotChangeInventory(t *testing.T) {
	h, db, token := agentServer(t, domain.ScopeAgent)
	report := baseReport()
	hosts := store.NewHostRepo(db)
	for range 2 {
		if _, err := hosts.Create(domain.Host{Name: report.Hostname, Type: "physical", Notes: "manual record"}); err != nil {
			t.Fatal(err)
		}
	}
	before, err := hosts.List()
	if err != nil {
		t.Fatal(err)
	}
	rec := postReport(t, h, token, report)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "multiple unbound hosts") {
		t.Fatalf("ambiguous report: %d %s", rec.Code, rec.Body.String())
	}
	after, err := hosts.List()
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("ambiguous report changed inventory: %+v, %v", after, err)
	}
	bound, err := store.NewAgentRepo(db).BoundHostIDs()
	if err != nil || len(bound) != 0 {
		t.Fatalf("ambiguous report created a binding: %v, %v", bound, err)
	}
	// Correcting inventory is sufficient; no agent reset or new identity needed.
	other := before[1]
	other.Name = "different machine"
	if err := hosts.Update(other); err != nil {
		t.Fatal(err)
	}
	rec = postReport(t, h, token, report)
	if rec.Code != http.StatusOK {
		t.Fatalf("report after disambiguation: %d %s", rec.Code, rec.Body.String())
	}
}
