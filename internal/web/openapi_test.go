package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

// everyEntityBase is the /api base for every catalog entity. If a new entity is
// added, this list (and the catalog) grow together; the coverage test then
// checks the spec includes it — with no per-entity spec code.
var everyEntityBase = []string{
	"/api/hosts", "/api/services", "/api/networks", "/api/domains",
	"/api/certificates", "/api/backups", "/api/hardware", "/api/subscriptions",
	"/api/accounts", "/api/sites", "/api/locations", "/api/racks",
	"/api/contacts", "/api/vlans", "/api/reservations",
}

func TestOpenAPISpecServed(t *testing.T) {
	h := newTestServer(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/openapi.json", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content-type = %q", ct)
	}
	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("unmarshal spec: %v", err)
	}
	if doc["openapi"] != "3.0.3" {
		t.Errorf("openapi = %v, want 3.0.3", doc["openapi"])
	}
	info, _ := doc["info"].(map[string]any)
	if info["title"] != "Almanaut API" {
		t.Errorf("info.title = %v", info["title"])
	}
	if info["version"] == "" || info["version"] == nil {
		t.Errorf("info.version missing")
	}
	// Bearer security scheme present and applied globally.
	comps, _ := doc["components"].(map[string]any)
	schemes, _ := comps["securitySchemes"].(map[string]any)
	if _, ok := schemes["bearerToken"]; !ok {
		t.Errorf("securitySchemes.bearerToken missing")
	}
	if _, ok := doc["security"]; !ok {
		t.Errorf("top-level security missing")
	}
}

func TestOpenAPICoversEveryEntityRoute(t *testing.T) {
	h := newTestServer(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/openapi.json", nil))
	var doc struct {
		Paths      map[string]map[string]any `json:"paths"`
		Components struct {
			Schemas map[string]any `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, base := range everyEntityBase {
		item, ok := doc.Paths[base]
		if !ok {
			t.Errorf("missing collection path %q", base)
			continue
		}
		if _, ok := item["get"]; !ok {
			t.Errorf("%q missing GET", base)
		}
		if _, ok := item["post"]; !ok {
			t.Errorf("%q missing POST", base)
		}
		byID, ok := doc.Paths[base+"/{id}"]
		if !ok {
			t.Errorf("missing item path %q/{id}", base)
			continue
		}
		for _, m := range []string{"get", "put", "delete"} {
			if _, ok := byID[m]; !ok {
				t.Errorf("%q/{id} missing %s", base, m)
			}
		}
	}
	// Cross-entity routes are documented too.
	for _, p := range []string{"/api/search", "/api/relationships", "/metrics"} {
		if _, ok := doc.Paths[p]; !ok {
			t.Errorf("missing path %q", p)
		}
	}
}

// TestOpenAPIRefsResolve guards against dangling $ref targets: every referenced
// component schema must be defined. A broken ref makes the document invalid.
func TestOpenAPIRefsResolve(t *testing.T) {
	h := newTestServer(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/openapi.json", nil))
	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	comps, _ := doc["components"].(map[string]any)
	schemas, _ := comps["schemas"].(map[string]any)
	for _, ref := range collectRefs(doc) {
		name := strings.TrimPrefix(ref, "#/components/schemas/")
		if _, ok := schemas[name]; !ok {
			t.Errorf("dangling $ref %q (schema not defined)", ref)
		}
	}
	// Sanity: at least one entity schema present with custom_fields.
	host, ok := schemas["Host"].(map[string]any)
	if !ok {
		t.Fatalf("Host schema missing")
	}
	props, _ := host["properties"].(map[string]any)
	if _, ok := props["custom_fields"]; !ok {
		t.Errorf("Host schema missing custom_fields")
	}
	if _, ok := props["name"]; !ok {
		t.Errorf("Host schema missing name")
	}
}

func collectRefs(v any) []string {
	var out []string
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if k == "$ref" {
				if s, ok := val.(string); ok {
					out = append(out, s)
				}
				continue
			}
			out = append(out, collectRefs(val)...)
		}
	case []any:
		for _, e := range t {
			out = append(out, collectRefs(e)...)
		}
	}
	return out
}

// TestEntitySchemaReflection checks the reflection maps Go kinds the way the
// JSON API actually serializes them.
func TestEntitySchemaReflection(t *testing.T) {
	s := entitySchema(reflect.TypeOf(domain.Host{}))
	if s.Type != "object" {
		t.Fatalf("Host schema type = %q", s.Type)
	}
	get := func(name string) *oaSchema {
		t.Helper()
		v, ok := s.Properties.vals[name]
		if !ok {
			t.Fatalf("property %q missing", name)
		}
		return v
	}
	if get("id").Type != "integer" {
		t.Errorf("id type = %q", get("id").Type)
	}
	if get("name").Type != "string" {
		t.Errorf("name type = %q", get("name").Type)
	}
	ips := get("ips")
	if ips.Type != "array" || ips.Items == nil || ips.Items.Type != "string" {
		t.Errorf("ips schema = %+v", ips)
	}
	live := get("liveness")
	if !live.Nullable || live.Type != "object" {
		t.Errorf("liveness should be nullable object, got %+v", live)
	}
	cf := get("custom_fields")
	if cf.Type != "object" || cf.AdditionalProperties == nil {
		t.Errorf("custom_fields schema = %+v", cf)
	}
}

func TestJSONFieldNameSkipsAndFallsBack(t *testing.T) {
	type sample struct {
		Kept    string `json:"kept"`
		Omitted string `json:"omitted,omitempty"`
		Skipped string `json:"-"`
		NoTag   string
	}
	rt := reflect.TypeOf(sample{})
	cases := map[string]struct {
		want string
		skip bool
	}{
		"Kept":    {"kept", false},
		"Omitted": {"omitted", false},
		"Skipped": {"", true},
		"NoTag":   {"NoTag", false},
	}
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		name, skip := jsonFieldName(f)
		c := cases[f.Name]
		if skip != c.skip || (!skip && name != c.want) {
			t.Errorf("%s: got (%q,%v), want (%q,%v)", f.Name, name, skip, c.want, c.skip)
		}
	}
}

func TestAPIDocsPageRenders(t *testing.T) {
	h := newTestServer(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/docs", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("content-type = %q", ct)
	}
	body := rec.Body.String()
	for _, want := range []string{"API docs", "/api/hosts", "custom_fields", "openapi.json", "Host"} {
		if !strings.Contains(body, want) {
			t.Errorf("docs page missing %q", want)
		}
	}
}
