package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestAgentProtectionKeepsManualValuesAndShowsObservations(t *testing.T) {
	h, db, token := agentServer(t, domain.ScopeAgent)
	report := baseReport()
	if rec := postReport(t, h, token, report); rec.Code != http.StatusOK {
		t.Fatalf("first report: %d %s", rec.Code, rec.Body)
	}
	hosts := store.NewHostRepo(db)
	all, err := hosts.List()
	if err != nil || len(all) != 1 {
		t.Fatalf("hosts: %+v %v", all, err)
	}
	id := all[0].ID
	path := "/hosts/" + itoa(id)
	editor := seedUserAndLogin(t, h, db, "editor", domain.RoleEditor)
	viewer := seedUserAndLogin(t, h, db, "viewer", domain.RoleViewer)
	form := url.Values{
		"name": {"nas01"}, "type": {"physical"}, "os": {"Manually documented OS"},
		"ips": {"192.168.1.10,192.168.1.99"}, "agent_protection_present": {"1"},
		"agent_protect_ips": {"on"}, "agent_protect_os": {"on"},
	}
	if rec := csrfPostRec(t, h, editor, path, form.Encode()); rec.Code != http.StatusSeeOther {
		t.Fatalf("protect: %d %s", rec.Code, rec.Body)
	}
	if rec := csrfPostRec(t, h, viewer, path, "name=nas01&type=physical&agent_protection_present=1"); rec.Code != http.StatusForbidden {
		t.Fatalf("viewer policy write: %d", rec.Code)
	}
	edit := getWith(t, h, editor, path+"/edit")
	if edit.Code != http.StatusOK || !strings.Contains(edit.Body.String(), `name="agent_protect_ips" checked`) {
		t.Fatalf("saved checkbox missing: %d %s", edit.Code, edit.Body)
	}
	// A failed edit retains both protection selections before the user retries.
	form.Set("name", "")
	invalid := csrfPostRec(t, h, editor, path, form.Encode())
	if !strings.Contains(invalid.Body.String(), `name="agent_protect_os" checked`) {
		t.Fatalf("invalid form lost protection: %s", invalid.Body)
	}
	report.OS = "Observed OS"
	report.RAM = "128 GB"
	report.Interfaces[0].Addrs = []string{"192.168.1.20"}
	for i := 0; i < 2; i++ {
		if rec := postReport(t, h, token, report); rec.Code != http.StatusOK {
			t.Fatalf("report: %d %s", rec.Code, rec.Body)
		}
	}
	stored, err := hosts.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if stored.OS != "Manually documented OS" || !reflect.DeepEqual(stored.IPs, []string{"192.168.1.10", "192.168.1.99"}) || stored.RAM != "128 GB" {
		t.Fatalf("wrong synchronization: %+v", stored)
	}
	payload, err := store.NewAgentRepo(db).LatestReport(id)
	if err != nil || !strings.Contains(payload, "Observed OS") {
		t.Fatalf("observations not retained: %s, %v", payload, err)
	}
	detail := getWith(t, h, viewer, path)
	for _, text := range []string{"Latest observed values", "Observed OS", "192.168.1.20", "Protected", "Manually documented OS"} {
		if !strings.Contains(detail.Body.String(), text) {
			t.Fatalf("detail missing %q", text)
		}
	}
	// Clearing all checkboxes is explicit and takes effect on the next report.
	form.Set("name", "nas01")
	form.Del("agent_protect_ips")
	form.Del("agent_protect_os")
	if rec := csrfPostRec(t, h, editor, path, form.Encode()); rec.Code != http.StatusSeeOther {
		t.Fatalf("clear: %d %s", rec.Code, rec.Body)
	}
	if rec := postReport(t, h, token, report); rec.Code != http.StatusOK {
		t.Fatalf("unprotected report: %d", rec.Code)
	}
	stored, err = hosts.Get(id)
	if err != nil || stored.OS != "Observed OS" || !reflect.DeepEqual(stored.IPs, []string{"192.168.1.20"}) {
		t.Fatalf("synchronization did not resume: %+v %v", stored, err)
	}
}

func TestAgentProtectedFieldsRemainStableIndependently(t *testing.T) {
	report := baseReport()
	for _, field := range []string{"ips", "os", "cpu", "ram", "disk"} {
		t.Run(field, func(t *testing.T) {
			before := domain.Host{OS: "manual OS", CPU: "manual CPU", RAM: "manual RAM", Disk: "manual disk", IPs: []string{"192.0.2.9"}, AgentProtectedFields: []string{field}}
			after := mergeAgentReport(before, report, false)
			oldJSON, _ := json.Marshal(before)
			newJSON, _ := json.Marshal(after)
			var oldFields, newFields map[string]any
			_ = json.Unmarshal(oldJSON, &oldFields)
			_ = json.Unmarshal(newJSON, &newFields)
			for _, name := range []string{"ips", "os", "cpu", "ram", "disk"} {
				equal := reflect.DeepEqual(oldFields[name], newFields[name])
				if equal != (name == field) {
					t.Fatalf("field %s, protecting %s: before=%v after=%v", name, field, oldFields[name], newFields[name])
				}
			}
		})
	}
}

func TestHostProtectionAPIPreservesOmittedAndNullPolicy(t *testing.T) {
	h, db, token := agentServer(t, domain.ScopeReadWrite)
	hosts := store.NewHostRepo(db)
	id, err := hosts.Create(domain.Host{Name: "server", Type: "physical", AgentProtectedFields: []string{"ips"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, extra string
		status      int
		protected   bool
	}{
		{"omitted", "", http.StatusOK, true},
		{"null", `,"agent_protected_fields":null`, http.StatusOK, true},
		{"unknown", `,"agent_protected_fields":["notes"]`, http.StatusBadRequest, true},
		{"duplicate", `,"agent_protected_fields":["ips","ips"]`, http.StatusBadRequest, true},
		{"clear", `,"agent_protected_fields":[]`, http.StatusOK, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPut, "/api/hosts/"+itoa(id), strings.NewReader(`{"name":"server","type":"physical"`+tc.extra+`}`))
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("PUT: %d %s", rec.Code, rec.Body)
			}
			stored, err := hosts.Get(id)
			if err != nil || stored.AgentProtects("ips") != tc.protected {
				t.Fatalf("stored policy: %+v %v", stored, err)
			}
			if rec.Code == http.StatusOK {
				var response domain.Host
				if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil || response.AgentProtects("ips") != tc.protected {
					t.Fatalf("response policy differs from storage: %+v %v", response, err)
				}
			}
		})
	}
	events, err := store.NewChangelogRepo(db).ListForEntity("host", id)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || len(events[0].Changes) != 1 || events[0].Changes[0].Field != "agent_protected_fields" {
		t.Fatalf("history should contain only explicit clearing: %+v", events)
	}
}

func TestHostProtectionCSVPresenceAndValidation(t *testing.T) {
	rs, deps := hostResourceForTest(t)
	created, _, rowErrs, err := rs.importCSV(deps, strings.NewReader("name,type,agent_protected_fields\nserver,physical,\"ips,os\"\n"), "editor")
	if err != nil || len(rowErrs) != 0 || created != 1 {
		t.Fatalf("create: %d %v %v", created, rowErrs, err)
	}
	hosts, err := rs.repo.List()
	if err != nil || len(hosts) != 1 {
		t.Fatalf("hosts: %+v %v", hosts, err)
	}
	id := hosts[0].ID
	for _, tc := range []struct {
		name, csv          string
		invalid, protected bool
	}{
		{"omitted", "id,name,type\n" + itoa(id) + ",server,physical\n", false, true},
		{"invalid", "id,name,type,agent_protected_fields\n" + itoa(id) + ",server,physical,bogus\n", true, true},
		{"clear", "id,name,type,agent_protected_fields\n" + itoa(id) + ",server,physical,\n", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, rows, err := rs.importCSV(deps, strings.NewReader(tc.csv), "editor")
			if err != nil || (len(rows) > 0) != tc.invalid {
				t.Fatalf("import: %v %v", rows, err)
			}
			host, err := rs.repo.Get(id)
			if err != nil || host.AgentProtects("ips") != tc.protected {
				t.Fatalf("policy: %+v %v", host, err)
			}
		})
	}
}

func TestHostProtectionSurvivesLegacyFormValidationRetry(t *testing.T) {
	for _, explicitClear := range []bool{false, true} {
		name := "legacy omission"
		if explicitClear {
			name = "explicit clear"
		}
		t.Run(name, func(t *testing.T) {
			h, db, _ := agentServer(t, domain.ScopeAgent)
			hosts := store.NewHostRepo(db)
			id, err := hosts.Create(domain.Host{
				Name: "server", Type: "physical", OS: "saved OS",
				AgentProtectedFields: []string{"ips", "os"},
			})
			if err != nil {
				t.Fatal(err)
			}
			editor := seedUserAndLogin(t, h, db, "editor", domain.RoleEditor)
			path := "/hosts/" + itoa(id)
			form := url.Values{"name": {""}, "type": {"physical"}, "os": {"draft OS"}}
			if explicitClear {
				form.Set("agent_protection_present", "1")
			}
			rec := csrfPostRec(t, h, editor, path, form.Encode())
			body := rec.Body.String()
			if rec.Code != http.StatusOK || !strings.Contains(body, "name is required") || !strings.Contains(body, `name="os" value="draft OS"`) {
				t.Fatalf("invalid draft was not preserved: %d %s", rec.Code, body)
			}
			if !strings.Contains(body, `name="agent_protection_present" value="1"`) {
				t.Fatal("redisplayed form lacks explicit policy marker")
			}
			// Submit the corrected name with the protection selections rendered
			// into the replacement form, as a browser would on the next submit.
			form.Set("name", "corrected")
			form.Set("agent_protection_present", "1")
			for _, field := range []string{"ips", "os", "cpu", "ram", "disk"} {
				checked := strings.Contains(body, `name="agent_protect_`+field+`" checked`)
				wantChecked := !explicitClear && (field == "ips" || field == "os")
				if checked != wantChecked {
					t.Fatalf("redisplayed %s checked=%v, want %v", field, checked, wantChecked)
				}
				if checked {
					form.Set("agent_protect_"+field, "on")
				}
			}
			stored, err := hosts.Get(id)
			if err != nil || stored.Name != "server" || !stored.AgentProtects("ips") {
				t.Fatalf("failed submission changed inventory: %+v %v", stored, err)
			}
			if rec := csrfPostRec(t, h, editor, path, form.Encode()); rec.Code != http.StatusSeeOther {
				t.Fatalf("corrected retry: %d %s", rec.Code, rec.Body)
			}
			stored, err = hosts.Get(id)
			if err != nil || stored.Name != "corrected" || stored.OS != "draft OS" || stored.AgentProtects("ips") == explicitClear || stored.AgentProtects("os") == explicitClear {
				t.Fatalf("retry changed intended policy or draft values: %+v %v", stored, err)
			}
		})
	}
}
