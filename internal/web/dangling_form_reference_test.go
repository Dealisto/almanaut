package web

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// selectedFormOption follows a single-select's browser default: its explicitly
// selected option, or the first option if no selection is marked.
func selectedFormOption(t *testing.T, body, name string) string {
	t.Helper()
	selectMatch := regexp.MustCompile(`(?s)<select name="` + regexp.QuoteMeta(name) + `">(.*?)</select>`).FindStringSubmatch(body)
	if len(selectMatch) != 2 {
		t.Fatalf("missing %s selector", name)
	}
	options := regexp.MustCompile(`<option value="([^"]*)"([^>]*)>`).FindAllStringSubmatch(selectMatch[1], -1)
	if len(options) == 0 {
		t.Fatalf("empty %s selector", name)
	}
	value := options[0][1]
	for _, option := range options {
		if strings.Contains(option[2], "selected") {
			value = option[1]
		}
	}
	return value
}

func TestEditFormsPreserveDeletedReferences(t *testing.T) {
	for _, tc := range []struct {
		name, parentPath, childPath, selector, column, prefix string
		parentForm, childForm                                 url.Values
		optional                                              bool
	}{
		{"NIC host", "/hosts", "/nics", "host_id", "host_id", "", url.Values{"type": {"physical"}}, url.Values{"kind": {"onboard"}}, false},
		{"port host", "/hosts", "/ports", "owner", "owner_id", "host:", url.Values{"type": {"physical"}}, url.Values{}, false},
		{"port hardware", "/hardware", "/ports", "owner", "owner_id", "hardware:", url.Values{}, url.Values{}, false},
		{"host rack", "/racks", "/hosts", "rack_id", "rack_id", "", url.Values{"u_height": {"42"}}, url.Values{"type": {"physical"}, "rack_position": {"1"}, "u_height": {"1"}}, true},
		{"hardware rack", "/racks", "/hardware", "rack_id", "rack_id", "", url.Values{"u_height": {"42"}}, url.Values{"rack_position": {"1"}, "u_height": {"1"}}, true},
		{"network VLAN", "/vlans", "/networks", "vlan_id", "vlan_id", "", url.Values{"vid": {"10"}}, url.Values{"cidr": {"10.0.0.0/24"}}, true},
		{"location site", "/sites", "/locations", "site_id", "site_id", "", url.Values{}, url.Values{}, true},
		{"rack location", "/locations", "/racks", "location_id", "location_id", "", url.Values{}, url.Values{"u_height": {"42"}}, true},
		{"reservation network", "/networks", "/reservations", "network_id", "network_id", "", url.Values{"cidr": {"10.0.0.0/24"}}, url.Values{"start_ip": {"10.0.0.1"}, "end_ip": {"10.0.0.10"}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, db := newTestServerDB(t)
			for _, name := range []string{"deleted parent", "surviving parent"} {
				tc.parentForm.Set("name", name)
				if rec := postForm(t, srv, tc.parentPath, tc.parentForm); rec.Code != http.StatusSeeOther {
					t.Fatalf("create parent = %d: %s", rec.Code, rec.Body)
				}
			}
			tc.childForm.Set("name", "child")
			tc.childForm.Set(tc.selector, tc.prefix+"1")
			if rec := postForm(t, srv, tc.childPath, tc.childForm); rec.Code != http.StatusSeeOther {
				t.Fatalf("create child = %d: %s", rec.Code, rec.Body)
			}
			if rec := postForm(t, srv, tc.parentPath+"/1/delete", nil); rec.Code != http.StatusSeeOther {
				t.Fatalf("delete parent = %d", rec.Code)
			}
			assertReference := func(want int64) {
				t.Helper()
				var actual int64
				table := strings.TrimPrefix(tc.childPath, "/")
				if table == "reservations" {
					table = "ip_reservations"
				}
				query := fmt.Sprintf("SELECT %s FROM %s WHERE id = 1", tc.column, table)
				if err := db.QueryRow(query).Scan(&actual); err != nil {
					t.Fatal(err)
				}
				if actual != want {
					t.Fatalf("stored %s = %d, want %d", tc.column, actual, want)
				}
			}
			assertReference(1)
			body := getBody(t, srv, tc.childPath+"/1/edit")
			selected := selectedFormOption(t, body, tc.selector)
			if selected != tc.prefix+"1" || !strings.Contains(body, "(deleted)") {
				t.Fatalf("missing deleted reference option: selected %q", selected)
			}
			// Validation errors must preserve the same pending reference.
			tc.childForm.Set("name", "")
			tc.childForm.Set(tc.selector, selected)
			rec := postForm(t, srv, tc.childPath+"/1", tc.childForm)
			if got := selectedFormOption(t, rec.Body.String(), tc.selector); got != selected {
				t.Fatalf("validation error changed selection to %q", got)
			}
			assertReference(1)
			tc.childForm.Set("name", "child")
			tc.childForm.Set("notes", "Updated notes")
			if rec := postForm(t, srv, tc.childPath+"/1", tc.childForm); rec.Code != http.StatusSeeOther {
				t.Fatalf("save dangling reference = %d: %s", rec.Code, rec.Body)
			}
			assertReference(1)
			// A deliberate replacement, and clearing an optional reference, work.
			tc.childForm.Set(tc.selector, tc.prefix+"2")
			if rec := postForm(t, srv, tc.childPath+"/1", tc.childForm); rec.Code != http.StatusSeeOther {
				t.Fatalf("replace reference = %d: %s", rec.Code, rec.Body)
			}
			assertReference(2)
			if got := selectedFormOption(t, getBody(t, srv, tc.childPath+"/1/edit"), tc.selector); got != tc.prefix+"2" {
				t.Fatalf("existing reference selection = %q", got)
			}
			if tc.optional {
				tc.childForm.Set(tc.selector, "0")
				if rec := postForm(t, srv, tc.childPath+"/1", tc.childForm); rec.Code != http.StatusSeeOther {
					t.Fatalf("clear reference = %d", rec.Code)
				}
				assertReference(0)
			}
		})
	}
}
