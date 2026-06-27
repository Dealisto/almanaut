package domain

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestYAMLSnakeCaseTags(t *testing.T) {
	out, err := yaml.Marshal(Certificate{ID: 1, Subject: "example.com", ExpiresOn: "2027-01-01", AutoRenew: true})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(out)
	for _, key := range []string{"expires_on:", "auto_renew:", "subject:"} {
		if !strings.Contains(s, key) {
			t.Errorf("missing snake_case key %q in:\n%s", key, s)
		}
	}

	rel, _ := yaml.Marshal(Relationship{ID: 1, FromType: "service", FromID: 1, ToType: "host", ToID: 2, Kind: "runs on"})
	for _, key := range []string{"from_type:", "from_id:", "to_type:", "to_id:"} {
		if !strings.Contains(string(rel), key) {
			t.Errorf("missing relationship key %q in:\n%s", key, rel)
		}
	}
}

func TestYAMLEmptySliceNotNull(t *testing.T) {
	out, _ := yaml.Marshal(Host{ID: 1, Name: "h", Type: "vm", IPs: nil})
	if !strings.Contains(string(out), "ips: []") {
		t.Errorf("nil IPs must marshal as [], got:\n%s", out)
	}
}
