package web

import (
	"context"
	"errors"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/discovery"
	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

type changingContainerScanner struct {
	containers []discovery.Container
	err        error
}

func (s *changingContainerScanner) Containers(context.Context) ([]discovery.Container, error) {
	return s.containers, s.err
}

func discoveryChangesForm(t *testing.T, body, externalID string, selected ...string) url.Values {
	t.Helper()
	for _, form := range regexp.MustCompile(`(?s)<form[^>]*>.*?</form>`).FindAllString(body, -1) {
		values := url.Values{}
		for _, m := range regexp.MustCompile(`name="(external_id|entity_type|entity_id|expected_[^"]+)" value="([^"]*)"`).FindAllStringSubmatch(form, -1) {
			values.Set(m[1], html.UnescapeString(m[2]))
		}
		if values.Get("external_id") == externalID {
			for _, name := range selected {
				values.Add("field", name)
			}
			return values
		}
	}
	t.Fatalf("no change form for %s", externalID)
	return nil
}

func TestDiscoveryChangesApplyOnlySelectedServiceFields(t *testing.T) {
	scanner := &changingContainerScanner{containers: []discovery.Container{{ID: "container", Name: "app", Image: "app:v2", ComposeProject: "observed-project", Ports: []discovery.Port{{Public: 9000, Private: 80, Proto: "tcp"}}}}}
	source := DiscoverySource{ID: "lab", Name: "Lab", Kind: "podman", Docker: scanner}
	srv, db := xenOrchestraTestServer(t, nil, false, false, []DiscoverySource{source})
	service := domain.Service{Name: "manual app", Kind: "container", Ports: "8000:80/tcp", Category: "manual category", URL: "https://app.example", Notes: "Discovered from a Docker-compatible API. Image: app:v1\nKeep my instructions."}
	id, err := store.NewServiceRepo(db).Create(service)
	if err != nil {
		t.Fatal(err)
	}
	service.ID = id
	if err := store.NewDiscoveryBindingRepo(db).Create(domain.DiscoveryBinding{Source: source.Key(), ExternalID: "container", EntityType: "service", EntityID: id}); err != nil {
		t.Fatal(err)
	}
	hostID, err := store.NewHostRepo(db).Create(domain.Host{Name: "host", Type: "physical"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.NewRelationshipRepo(db).Create(domain.Relationship{FromType: "service", FromID: id, ToType: "host", ToID: hostID, Kind: "runs on"}); err != nil {
		t.Fatal(err)
	}
	beforeRels, _ := store.NewRelationshipRepo(db).List()
	body := getBody(t, srv, source.Path()+"/changes")
	for _, want := range []string{"app:v1", "app:v2", "Container image (informational)", "/services/" + strconv.FormatInt(id, 10), "Last observed"} {
		if !strings.Contains(body, want) {
			t.Errorf("review missing %q", want)
		}
	}
	if strings.Contains(body, `name="expected_image"`) || strings.Contains(body, `value="ports" aria-label="Apply Published ports" checked`) {
		t.Fatal("image editable or fields preselected")
	}
	form := discoveryChangesForm(t, body, "container", "ports")
	rec := postForm(t, srv, source.Path()+"/changes", form)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `data-changes-applied="1"`) {
		t.Fatalf("apply %d: %s", rec.Code, rec.Body.String())
	}
	got, err := store.NewServiceRepo(db).Get(id)
	if err != nil {
		t.Fatal(err)
	}
	service.Ports = "9000:80/tcp"
	if !reflect.DeepEqual(got, service) {
		t.Fatalf("unselected fields modified:\ngot %#v\nwant %#v", got, service)
	}
	afterRels, _ := store.NewRelationshipRepo(db).List()
	if !reflect.DeepEqual(beforeRels, afterRels) {
		t.Fatal("relations changed")
	}
	events, err := store.NewChangelogRepo(db).ListForEntity("service", id)
	if err != nil || len(events) != 1 || len(events[0].Changes) != 1 || events[0].Changes[0].Field != "ports" {
		t.Fatalf("audit = %+v, %v", events, err)
	}
}

func TestDiscoveryChangesRejectStaleLocalRemoteAndBinding(t *testing.T) {
	for _, change := range []string{"local", "remote", "binding", "missing", "duplicate", "unknown-field", "image"} {
		t.Run(change, func(t *testing.T) {
			scanner := &changingContainerScanner{containers: []discovery.Container{{ID: "c", Name: "app", Ports: []discovery.Port{{Public: 9000, Private: 80, Proto: "tcp"}}}}}
			source := DiscoverySource{ID: "lab", Name: "Lab", Kind: "docker", Docker: scanner}
			srv, db := xenOrchestraTestServer(t, nil, false, false, []DiscoverySource{source})
			id, err := store.NewServiceRepo(db).Create(domain.Service{Name: "app", Kind: "container", Ports: "8000:80/tcp"})
			if err != nil {
				t.Fatal(err)
			}
			binding := domain.DiscoveryBinding{Source: source.Key(), ExternalID: "c", EntityType: "service", EntityID: id}
			if err := store.NewDiscoveryBindingRepo(db).Create(binding); err != nil {
				t.Fatal(err)
			}
			form := discoveryChangesForm(t, getBody(t, srv, source.Path()+"/changes"), "c", "ports")
			switch change {
			case "local":
				s, _ := store.NewServiceRepo(db).Get(id)
				s.Ports = "manual-edit"
				if err := store.NewServiceRepo(db).Update(s); err != nil {
					t.Fatal(err)
				}
			case "remote":
				scanner.containers[0].Ports[0].Public = 9999
			case "binding":
				form.Set("entity_id", strconv.FormatInt(id+1, 10))
			case "missing":
				scanner.containers = nil
			case "duplicate":
				scanner.containers = append(scanner.containers, scanner.containers[0])
			case "unknown-field":
				form.Set("field", "notes")
			case "image":
				form.Set("field", "image")
			}
			before, _ := store.NewServiceRepo(db).Get(id)
			rec := postForm(t, srv, source.Path()+"/changes", form)
			if rec.Code != http.StatusConflict {
				t.Fatalf("stale %s = %d %s", change, rec.Code, rec.Body.String())
			}
			after, _ := store.NewServiceRepo(db).Get(id)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("stale update wrote inventory")
			}
			events, _ := store.NewChangelogRepo(db).ListForEntity("service", id)
			if len(events) != 0 {
				t.Fatal("stale update wrote changelog")
			}
		})
	}
}

func TestDiscoveryChangesHostFieldsAndUnavailableAddresses(t *testing.T) {
	scanner := &fakeXenOrchestraScanner{resources: []discovery.XenOrchestraResource{{ID: "vm", Name: "observed", Type: "vm", CPU: 4, Memory: 4 << 30, Status: "running"}}}
	source := DiscoverySource{ID: "lab", Name: "Lab", Kind: "xen-orchestra", XenOrchestra: scanner}
	srv, db := xenOrchestraTestServer(t, nil, false, false, []DiscoverySource{source})
	id, err := store.NewHostRepo(db).Create(domain.Host{Name: "manual", Type: "vm", CPU: "2 cores", RAM: "2.0 GiB", Status: "stopped", IPs: []string{"192.0.2.50"}, OS: "Manual OS", Notes: "Keep"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.NewDiscoveryBindingRepo(db).Create(domain.DiscoveryBinding{Source: source.Key(), ExternalID: "vm", EntityType: "host", EntityID: id}); err != nil {
		t.Fatal(err)
	}
	body := getBody(t, srv, source.Path()+"/changes")
	if strings.Contains(body, `name="expected_ips"`) {
		t.Fatal("unavailable addresses offered for removal")
	}
	form := discoveryChangesForm(t, body, "vm", "cpu", "ram", "status")
	// An unrelated manual edit after preview must survive acceptance.
	h, _ := store.NewHostRepo(db).Get(id)
	h.Notes = "A newer manual note"
	if err := store.NewHostRepo(db).Update(h); err != nil {
		t.Fatal(err)
	}
	rec := postForm(t, srv, source.Path()+"/changes", form)
	if rec.Code != 200 {
		t.Fatalf("apply host = %d %s", rec.Code, rec.Body.String())
	}
	got, _ := store.NewHostRepo(db).Get(id)
	if got.CPU != "4 cores" || got.RAM != "4.0 GiB" || got.Status != "running" || got.Name != "manual" || got.OS != "Manual OS" || got.Notes != "A newer manual note" || !reflect.DeepEqual(got.IPs, []string{"192.0.2.50"}) {
		t.Fatalf("host changes = %+v", got)
	}
	scanner.resources[0].IPs = []string{"192.0.2.51"}
	form = discoveryChangesForm(t, getBody(t, srv, source.Path()+"/changes"), "vm", "ips")
	if rec := postForm(t, srv, source.Path()+"/changes", form); rec.Code != 200 {
		t.Fatalf("apply IP = %d", rec.Code)
	}
	got, _ = store.NewHostRepo(db).Get(id)
	if !reflect.DeepEqual(got.IPs, []string{"192.0.2.51"}) {
		t.Fatalf("IP changes = %v", got.IPs)
	}
}

func TestDiscoveryChangesMissingAndFailedScansPreserveObservation(t *testing.T) {
	scanner := &changingContainerScanner{containers: []discovery.Container{{ID: "c", Name: "app"}}}
	source := DiscoverySource{ID: "lab", Name: "Lab", Kind: "docker", Docker: scanner}
	srv, db := xenOrchestraTestServer(t, nil, false, false, []DiscoverySource{source})
	id, err := store.NewServiceRepo(db).Create(domain.Service{Name: "app", Kind: "container"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.NewDiscoveryBindingRepo(db).Create(domain.DiscoveryBinding{Source: source.Key(), ExternalID: "c", EntityType: "service", EntityID: id}); err != nil {
		t.Fatal(err)
	}
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r := store.NewDiscoveryObservationRepo(db)
	if err := r.RecordBound(source.Key(), []string{"c"}, old); err != nil {
		t.Fatal(err)
	}
	scanner.containers = nil
	body := getBody(t, srv, source.Path()+"/changes")
	if !strings.Contains(body, "Not observed") || !strings.Contains(body, "running containers only") {
		t.Fatal("missing resource lacks explanation")
	}
	seen, err := r.ListSource(source.Key())
	if err != nil || seen["c"] != old.Format(time.RFC3339) {
		t.Fatalf("missing scan overwrote last seen: %v %v", seen, err)
	}
	scanner.err = errors.New("test source unavailable")
	body = getBody(t, srv, source.Path()+"/changes")
	if !strings.Contains(body, "test source unavailable") || strings.Contains(body, "<strong>Not observed</strong>") {
		t.Fatal("failed scan reports missing resources")
	}
	seen, err = r.ListSource(source.Key())
	if err != nil || seen["c"] != old.Format(time.RFC3339) {
		t.Fatalf("failure overwrote last seen: %v %v", seen, err)
	}
	scanner.err = nil
	scanner.containers = []discovery.Container{{ID: "c", Name: "app"}}
	body = getBody(t, srv, source.Path()+"/changes")
	if strings.Contains(body, "<strong>Not observed</strong>") {
		t.Fatal("returning resource remains missing")
	}
	seen, err = r.ListSource(source.Key())
	if err != nil || seen["c"] == old.Format(time.RFC3339) {
		t.Fatalf("success did not refresh observation: %v %v", seen, err)
	}
	if _, err := store.NewServiceRepo(db).Get(id); err != nil {
		t.Fatal("missing resource inventory removed", err)
	}
}

func TestDiscoveryChangesRequireAuthenticationAndCSRF(t *testing.T) {
	source := DiscoverySource{ID: "lab", Name: "Lab", Kind: "docker", Docker: &changingContainerScanner{}}
	srv, _ := xenOrchestraTestServer(t, nil, false, true, []DiscoverySource{source})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, source.Path()+"/changes", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("anonymous review = %d", rec.Code)
	}
	srv, _ = xenOrchestraTestServer(t, nil, false, false, []DiscoverySource{source})
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, source.Path()+"/changes", strings.NewReader("field=ports"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF = %d", rec.Code)
	}
}
