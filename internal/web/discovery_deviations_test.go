package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/discovery"
	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

type deviationsScanner struct {
	calls      int
	containers []discovery.Container
	err        error
}

func (s *deviationsScanner) Containers(context.Context) ([]discovery.Container, error) {
	s.calls++
	return s.containers, s.err
}

func TestDiscoveryDeviationsCachedReviewAndSafeApply(t *testing.T) {
	scanner := &deviationsScanner{containers: []discovery.Container{{ID: "container", Name: "app", Ports: []discovery.Port{{Public: 9000, Private: 80, Proto: "tcp"}}}}}
	source := DiscoverySource{ID: "lab", Name: "Lab <script>", Kind: "docker", Docker: scanner}
	srv, db := xenOrchestraTestServer(t, nil, false, false, []DiscoverySource{source})
	id, err := store.NewServiceRepo(db).Create(domain.Service{Name: "app", Kind: "container", Ports: "8000:80/tcp", Notes: "Keep me"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.NewDiscoveryBindingRepo(db).Create(domain.DiscoveryBinding{Source: source.Key(), ExternalID: "container", EntityType: "service", EntityID: id}); err != nil {
		t.Fatal(err)
	}
	empty := getBody(t, srv, "/discovery/deviations")
	if scanner.calls != 0 || !strings.Contains(empty, "No successful scan") || strings.Contains(empty, `data-deviation-state="missing"`) {
		t.Fatal("unscanned source falsely missing or contacted")
	}
	getBody(t, srv, source.Path()+"/changes")
	body := getBody(t, srv, "/discovery/deviations")
	if scanner.calls != 1 || !strings.Contains(body, `data-deviation-state="changed"`) || !strings.Contains(body, "9000:80/tcp") || !strings.Contains(body, "Lab &lt;script&gt;") {
		t.Fatalf("cached review mismatch (%d scans): %s", scanner.calls, body)
	}
	form := discoveryChangesForm(t, body, "container", "ports")
	rec := postForm(t, srv, source.Path()+"/changes", form)
	if rec.Code != http.StatusOK || scanner.calls != 2 {
		t.Fatalf("fresh apply = %d, calls %d", rec.Code, scanner.calls)
	}
	got, _ := store.NewServiceRepo(db).Get(id)
	if got.Ports != "9000:80/tcp" || got.Notes != "Keep me" {
		t.Fatalf("unsafe apply: %+v", got)
	}
	if strings.Contains(getBody(t, srv, "/discovery/deviations"), `data-deviation-state="changed"`) {
		t.Fatal("applied deviation remains despite unchanged cached source")
	}
}

func TestDiscoveryDeviationsFailedScanDoesNotImplyMissing(t *testing.T) {
	scanner := &deviationsScanner{containers: []discovery.Container{{ID: "one", Name: "app"}}}
	source := DiscoverySource{ID: "lab", Name: "Lab", Kind: "docker", Docker: scanner}
	srv, db := xenOrchestraTestServer(t, nil, false, false, []DiscoverySource{source})
	id, err := store.NewServiceRepo(db).Create(domain.Service{Name: "app", Kind: "container"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.NewDiscoveryBindingRepo(db).Create(domain.DiscoveryBinding{Source: source.Key(), ExternalID: "one", EntityType: "service", EntityID: id}); err != nil {
		t.Fatal(err)
	}
	getBody(t, srv, source.Path()+"/changes")
	scanner.err = errors.New("offline")
	getBody(t, srv, source.Path()+"/changes")
	body := getBody(t, srv, "/discovery/deviations")
	if !strings.Contains(body, "Latest scan failed") || strings.Contains(body, `data-deviation-state="missing"`) {
		t.Fatal("failure treated as disappearance", body)
	}
	scanner.err = nil
	scanner.containers = nil
	getBody(t, srv, source.Path()+"/changes")
	body = getBody(t, srv, "/discovery/deviations")
	if !strings.Contains(body, `data-deviation-state="missing"`) || !strings.Contains(body, "This does not mean the resource was deleted") {
		t.Fatal("successful empty scan boundary missing", body)
	}
	if _, err := store.NewServiceRepo(db).Get(id); err != nil {
		t.Fatal("missing resource was removed", err)
	}
}

func TestDiscoveryDeviationsSourceIsolationFiltersAndStaleness(t *testing.T) {
	a := DiscoverySource{ID: "a", Name: "Alpha", Kind: "docker", Docker: &deviationsScanner{}}
	b := DiscoverySource{ID: "b", Name: "Beta", Kind: "podman", Docker: &deviationsScanner{}}
	srv, db := xenOrchestraTestServer(t, nil, false, false, []DiscoverySource{a, b})
	repo := store.NewDiscoverySnapshotRepo(db)
	for _, source := range []DiscoverySource{a, b} {
		if err := repo.Record(source.Key(), time.Now().Add(-25*time.Hour), []discovery.Candidate{{ID: "same-id", EntityType: "service", Service: domain.Service{Name: source.Name + " resource", Kind: "container"}}}, nil); err != nil {
			t.Fatal(err)
		}
	}
	body := getBody(t, srv, "/discovery/deviations?source="+url.QueryEscape(b.Key())+"&state=new&q=Beta")
	if !strings.Contains(body, "Beta resource") || strings.Contains(body, "Alpha resource") || !strings.Contains(body, "Stale observation") {
		t.Fatal("scoped cached source mismatch", body)
	}
	for _, query := range []string{"source=unknown", "state=bogus", "page=0", "page=999999999999999999999999"} {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/discovery/deviations?"+query, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s = %d", query, rec.Code)
		}
	}
}

func TestDiscoveryDeviationsRequireAuthentication(t *testing.T) {
	source := DiscoverySource{ID: "lab", Name: "Lab", Kind: "docker", Docker: &deviationsScanner{}}
	srv, _ := xenOrchestraTestServer(t, nil, false, true, []DiscoverySource{source})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/discovery/deviations", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("anonymous review=%d", rec.Code)
	}
}

func TestDiscoveryDeviationsPagination(t *testing.T) {
	source := DiscoverySource{ID: "lab", Name: "Lab", Kind: "docker", Docker: &deviationsScanner{}}
	srv, db := xenOrchestraTestServer(t, nil, false, false, []DiscoverySource{source})
	var candidates []discovery.Candidate
	for i := 0; i < 101; i++ {
		candidates = append(candidates, discovery.Candidate{ID: time.Unix(int64(i), 0).String(), EntityType: "service", Service: domain.Service{Name: "app", Kind: "container"}})
	}
	if err := store.NewDiscoverySnapshotRepo(db).Record(source.Key(), time.Now(), candidates, nil); err != nil {
		t.Fatal(err)
	}
	first := getBody(t, srv, "/discovery/deviations?state=new")
	if strings.Count(first, `data-deviation-state="new"`) != 100 || !strings.Contains(first, "Next page") {
		t.Fatal("first page not bounded")
	}
	second := getBody(t, srv, "/discovery/deviations?state=new&page=2")
	if strings.Count(second, `data-deviation-state="new"`) != 1 || !strings.Contains(second, "Previous page") {
		t.Fatal("second page mismatch")
	}
}
