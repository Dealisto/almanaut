package web

import (
	"context"
	"errors"
	"html"
	"net/http"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestDetailRelationshipsIgnoreUnrelatedInventory(t *testing.T) {
	srv, db := newTestServerDB(t)
	if _, err := store.NewServiceRepo(db).Create(domain.Service{Name: "application", Kind: "container"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.NewHardwareRepo(db).Create(domain.Hardware{Name: "switch", Kind: "switch"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.NewRelationshipRepo(db).Create(domain.Relationship{FromType: "service", FromID: 1, ToType: "hardware", ToID: 1, Kind: "depends on"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP TABLE certificates`); err != nil {
		t.Fatal(err)
	}
	response := getJSONResp(t, srv, "/services/1")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "hardware: switch") || !strings.Contains(response.Body.String(), `href="/hardware/1"`) {
		t.Fatalf("unrelated inventory broke detail relationships: status=%d body=%s", response.Code, response.Body)
	}
}

func TestDetailRelationshipsPreserveDirectionsOrderAndFallbacks(t *testing.T) {
	srv, db := newTestServerDB(t)
	if _, err := store.NewServiceRepo(db).Create(domain.Service{Name: "application", Kind: "container"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.NewHardwareRepo(db).Create(domain.Hardware{Name: "switch <&>", Kind: "switch"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.NewHostRepo(db).Create(domain.Host{Name: "server", Type: "physical"}); err != nil {
		t.Fatal(err)
	}
	rels := store.NewRelationshipRepo(db)
	// Include legacy self/duplicate/unknown edges that can exist in storage even
	// though normal form validation does not permit every one of them.
	for _, edge := range []domain.Relationship{
		{FromType: "service", FromID: 1, ToType: "hardware", ToID: 1, Kind: "depends on"},
		{FromType: "host", FromID: 1, ToType: "service", ToID: 1, Kind: "runs"},
		{FromType: "service", FromID: 1, ToType: "hardware", ToID: 1, Kind: "connects to"},
		{FromType: "service", FromID: 1, ToType: "service", ToID: 1, Kind: "self"},
		{FromType: "service", FromID: 1, ToType: "host", ToID: 999, Kind: "missing"},
		{FromType: "legacy", FromID: 7, ToType: "service", ToID: 1, Kind: "unknown"},
	} {
		if _, err := rels.Create(edge); err != nil {
			t.Fatal(err)
		}
	}
	response := getJSONResp(t, srv, "/services/1")
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body)
	}
	body := response.Body.String()
	start := strings.LastIndex(body, `<ul class="related">`)
	if start < 0 {
		t.Fatal("missing relationships list")
	}
	list := body[start:]
	list = list[:strings.Index(list, "</ul>")]
	previous := -1
	for _, want := range []string{
		"depends on → hardware: switch <&>",
		"host: server → runs",
		"connects to → hardware: switch <&>",
		"self → service: application",
		"missing → host:999 (deleted)",
		"legacy:7 (deleted) → unknown",
	} {
		index := strings.Index(html.UnescapeString(list), want)
		if index <= previous {
			t.Fatalf("missing or reordered %q in %s", want, list)
		}
		previous = index
	}
	if strings.Count(list, `href="/hardware/1"`) != 2 || !strings.Contains(list, `href="/hosts/1"`) || !strings.Contains(list, `href="/services/1"`) {
		t.Fatalf("existing endpoint links changed: %s", list)
	}
	if strings.Contains(list, `href="/hosts/999"`) || strings.Count(list, "<a ") != 4 {
		t.Fatalf("missing or unknown endpoint became linked: %s", list)
	}
}

func TestDetailRelationshipsEmptyAndStorageErrors(t *testing.T) {
	srv, db := newTestServerDB(t)
	if _, err := store.NewServiceRepo(db).Create(domain.Service{Name: "application", Kind: "container"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP TABLE hardware`); err != nil {
		t.Fatal(err)
	}
	if response := getJSONResp(t, srv, "/services/1"); response.Code != http.StatusOK {
		t.Fatalf("empty relationships read unrelated inventory: status=%d", response.Code)
	}
	if _, err := store.NewRelationshipRepo(db).Create(domain.Relationship{FromType: "service", FromID: 1, ToType: "hardware", ToID: 1, Kind: "depends on"}); err != nil {
		t.Fatal(err)
	}
	if response := getJSONResp(t, srv, "/services/1"); response.Code != http.StatusInternalServerError {
		t.Fatalf("referenced table error concealed: status=%d", response.Code)
	}
}

func TestDetailRelationshipsCanceledContext(t *testing.T) {
	_, db := newTestServerDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := detailRelationships(ctx, db, entityCatalog{}, store.NewRelationshipRepo(db), "service", 1)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled read error=%v", err)
	}
}
