package web

import (
	"errors"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/discovery"
	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/job"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestDiscoveryDashboardKeepsLastSuccessWhenLatestScanFails(t *testing.T) {
	db := rbacDB(t)
	runs := store.NewDiscoveryRunRepo(db)
	success := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	failure := success.Add(time.Hour)
	for _, run := range []store.DiscoveryRun{
		{Source: "docker:lab", StartedAt: success, FinishedAt: success, FoundCount: 7},
		{Source: "docker:lab", StartedAt: failure, FinishedAt: failure, Error: "socket unavailable <test>"},
		{Source: "docker:other", StartedAt: failure, FinishedAt: failure, FoundCount: 999},
	} {
		if _, err := runs.Record(run); err != nil {
			t.Fatal(err)
		}
	}
	runner := &fakeRunner{statuses: []job.Status{{Name: "discovery-docker-lab", NextRun: failure.Add(time.Hour)}}}
	sources := []DiscoverySource{
		{ID: "lab", Name: "Container lab", Kind: "docker", Docker: fakeScanner{err: errors.New("socket unavailable")}},
		{ID: "empty", Name: "Unscheduled source", Kind: "podman", Docker: fakeScanner{}},
	}
	rec := httptest.NewRecorder()
	discoverySourcesLanding(NetDiscoveryOptions{}, sources, runs, runner).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/discovery", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("landing: %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"Last scan failed", "2026-11-24 12:00:00", "2026-11-24 13:00:00", "2026-11-24 14:00:00", "Resources at last success</dt><dd>7</dd>", "socket unavailable &lt;test&gt;", `action="/tasks/discovery-docker-lab/run"`, "Automatic scans not scheduled", "Not scanned yet"} {
		if !strings.Contains(body, want) {
			t.Errorf("landing missing %q", want)
		}
	}
	if strings.Contains(body, "999") || strings.Contains(body, "<test>") {
		t.Fatal("unrelated source count or unescaped error leaked into landing")
	}
}

func TestDiscoveryDashboardControlsFollowTaskPermissions(t *testing.T) {
	source := DiscoverySource{ID: "default", Name: "Containers", Kind: "docker", Docker: fakeScanner{}}
	runner := &fakeRunner{statuses: []job.Status{{Name: "discovery-docker", Running: true}}}
	for _, role := range []domain.Role{domain.RoleAdmin, domain.RoleEditor, domain.RoleViewer} {
		t.Run(string(role), func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/discovery", nil)
			req = req.WithContext(withUser(req.Context(), domain.User{Username: "reviewer", Role: role}))
			rec := httptest.NewRecorder()
			discoverySourcesLanding(NetDiscoveryOptions{}, []DiscoverySource{source}, nil, runner).ServeHTTP(rec, req)
			if rec.Code != 200 {
				t.Fatalf("landing: %d", rec.Code)
			}
			body := rec.Body.String()
			hasControl := strings.Contains(body, `action="/tasks/discovery-docker/run"`)
			if hasControl != (role == domain.RoleAdmin) {
				t.Errorf("run control as %s = %v", role, hasControl)
			}
			if role == domain.RoleAdmin && !strings.Contains(body, `type="submit" disabled>Run now`) {
				t.Error("running scan must disable manual run control")
			}
		})
	}
	// The existing endpoint, not just its new dashboard button, must reject editors.
	db := rbacDB(t)
	handler := newTasksTestHandler(t, db, runner)
	editor := seedUserAndLogin(t, handler, db, "editor", domain.RoleEditor)
	if rec := csrfPostRec(t, handler, editor, "/tasks/discovery-docker/run", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("editor triggered scheduled scan: %d", rec.Code)
	}
	if len(runner.triggered) != 0 {
		t.Fatalf("unauthorized trigger: %v", runner.triggered)
	}
}

func TestDiscoveryDashboardHistoryReadFailureIsServerError(t *testing.T) {
	db := rbacDB(t)
	if _, err := db.Exec("DROP TABLE discovery_runs"); err != nil {
		t.Fatal(err)
	}
	source := DiscoverySource{ID: "default", Name: "Containers", Kind: "docker", Docker: fakeScanner{}}
	rec := httptest.NewRecorder()
	discoverySourcesLanding(NetDiscoveryOptions{}, []DiscoverySource{source}, store.NewDiscoveryRunRepo(db), nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/discovery", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("broken history returned %d instead of 500", rec.Code)
	}
}

func TestDiscoveryFiltersExcludeHiddenRowsFromImport(t *testing.T) {
	scanner := fakeScanner{containers: []discovery.Container{
		{ID: "new-resource", Name: "New application"},
		{ID: "match-resource", Name: "Manual application"},
		{ID: "tracked-resource", Name: "Renamed application"},
		{ID: "conflict-resource", Name: "Duplicate application"},
	}}
	source := DiscoverySource{ID: "lab", Name: "Container lab", Kind: "docker", Docker: scanner}
	handler, db := xenOrchestraTestServer(t, nil, false, false, []DiscoverySource{source})
	repo := store.NewServiceRepo(db)
	ids := make([]int64, 0, 4)
	for _, name := range []string{"Manual application", "Original application", "Duplicate application", "Duplicate application"} {
		id, err := repo.Create(domain.Service{Name: name, Kind: "container", Notes: "Preserve these details"})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := store.NewDiscoveryBindingRepo(db).Create(domain.DiscoveryBinding{Source: source.Key(), ExternalID: "tracked-resource", EntityType: "service", EntityID: ids[1]}); err != nil {
		t.Fatal(err)
	}
	for _, filter := range []string{"new", "matched", "tracked", "conflict"} {
		body := getBody(t, handler, source.Path()+"?status="+filter)
		if !strings.Contains(body, "Showing 1 of 4 resources.") || strings.Count(body, "data-discovery-status=") != 1 || !strings.Contains(body, `data-discovery-status="`+filter+`"`) {
			t.Errorf("incorrect filtered rows for %s", filter)
		}
		if filter == "tracked" && !strings.Contains(body, `href="/services/`+strconv.FormatInt(ids[1], 10)+`">Renamed application</a>`) {
			t.Error("tracked source name does not link to its existing local record")
		}
		if (filter == "tracked" || filter == "conflict") && strings.Contains(body, `name="id"`) {
			t.Errorf("filter %s offers a hidden or ineligible row for import", filter)
		}
	}
	filtered := getBody(t, handler, source.Path()+"?status=new&q=NEW")
	form := url.Values{}
	for _, match := range regexp.MustCompile(`name="id" value="([^"]+)"`).FindAllStringSubmatch(filtered, -1) {
		form.Add("id", html.UnescapeString(match[1]))
	}
	if got := form["id"]; len(got) != 1 || got[0] != "new-resource" {
		t.Fatalf("filtered form submitted hidden resources: %v", got)
	}
	assertImportCount(t, postForm(t, handler, source.Path()+"/import", form), "added", 1)
	bindings, err := store.NewDiscoveryBindingRepo(db).List()
	if err != nil || len(bindings) != 2 {
		t.Fatalf("unexpected bindings after visible-only import: %+v %v", bindings, err)
	}
	for _, binding := range bindings {
		if binding.ExternalID == "match-resource" {
			t.Fatal("hidden manual record was adopted")
		}
	}
	for _, id := range ids {
		item, err := repo.Get(id)
		if err != nil || item.Notes != "Preserve these details" {
			t.Fatalf("existing manual details changed: %+v %v", item, err)
		}
	}
}

func TestDiscoveryFiltersHaveDistinctEmptyAndUnavailableStates(t *testing.T) {
	for _, tc := range []struct {
		name, query, want string
		scanner           fakeScanner
	}{
		{"no matching rows", "?q=absent", "No matching resources.", fakeScanner{containers: []discovery.Container{{ID: "one", Name: "one"}}}},
		{"empty source", "", "No running containers found.", fakeScanner{}},
		{"unavailable source", "?status=tracked", "Could not reach the Docker / Podman socket: unavailable", fakeScanner{err: errors.New("unavailable")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := DiscoverySource{ID: "lab", Name: "Container lab", Kind: "docker", Docker: tc.scanner}
			handler, _ := xenOrchestraTestServer(t, nil, false, false, []DiscoverySource{source})
			body := getBody(t, handler, source.Path()+tc.query)
			if !strings.Contains(body, tc.want) {
				t.Fatalf("missing state %q", tc.want)
			}
			if tc.scanner.err != nil && strings.Contains(body, "No running containers found.") {
				t.Fatal("failed scan reported an empty source")
			}
		})
	}
	source := DiscoverySource{ID: "lab", Name: "Container lab", Kind: "docker", Docker: fakeScanner{}}
	handler, _ := xenOrchestraTestServer(t, nil, false, false, []DiscoverySource{source})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, source.Path()+"?status=invalid", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid filter: %d", rec.Code)
	}
}

func TestDiscoveryPreviewObservesHiddenTrackedRowsButNotFailedScans(t *testing.T) {
	scanner := &fakeXenOrchestraScanner{resources: xenOrchestraFixture()}
	source := DiscoverySource{ID: "lab", Name: "Lab", Kind: "xen-orchestra", XenOrchestra: scanner}
	handler, db := xenOrchestraTestServer(t, nil, false, false, []DiscoverySource{source})
	assertImportCount(t, postForm(t, handler, source.Path()+"/import", url.Values{"id": {"host-a", "vm-a"}}), "added", 2)
	observations := store.NewDiscoveryObservationRepo(db)
	old := time.Now().UTC().Add(-24 * time.Hour)
	if err := observations.RecordBound(source.Key(), []string{"host-a", "vm-a"}, old); err != nil {
		t.Fatal(err)
	}
	body := getBody(t, handler, source.Path()+"?status=new")
	if strings.Contains(body, "data-discovery-status=") {
		t.Fatal("tracked resources leaked through new-only filter")
	}
	seen, err := observations.ListSource(source.Key())
	if err != nil || len(seen) != 2 {
		t.Fatalf("hidden resources were not observed: %+v %v", seen, err)
	}
	for _, stamp := range seen {
		if stamp <= old.Format(time.RFC3339) {
			t.Fatalf("hidden resource retained old observation: %s", stamp)
		}
	}
	scanner.err = errors.New("source unavailable")
	getBody(t, handler, source.Path())
	after, err := observations.ListSource(source.Key())
	if err != nil || len(after) != len(seen) {
		t.Fatalf("failed scan lost previous observations: %+v %v", after, err)
	}
	for id, stamp := range seen {
		if after[id] != stamp {
			t.Fatalf("failed scan changed observation of %s", id)
		}
	}
}
