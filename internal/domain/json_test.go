package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEntitiesMarshalSnakeCase(t *testing.T) {
	cases := []struct {
		name string
		v    any
		want []string // substrings that must appear as JSON keys
	}{
		{"certificate", Certificate{ID: 1, Subject: "x", ExpiresOn: "2027-01-01", AutoRenew: true}, []string{`"expires_on"`, `"auto_renew"`, `"subject"`}},
		{"host", Host{ID: 1, Name: "h", IPs: []string{"10.0.0.1"}}, []string{`"ips"`, `"name"`}},
		{"account", Account{ID: 1, Name: "a", PasswordManager: "bw", SecretRef: "r"}, []string{`"password_manager"`, `"secret_ref"`}},
		{"relationship", Relationship{ID: 1, FromType: "service", FromID: 2, ToType: "host", ToID: 3, Kind: "runs on"}, []string{`"from_type"`, `"from_id"`, `"to_type"`, `"to_id"`}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b, err := json.Marshal(c.v)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			got := string(b)
			for _, w := range c.want {
				if !strings.Contains(got, w) {
					t.Errorf("missing key %s in %s", w, got)
				}
			}
		})
	}
}
