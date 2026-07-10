package domain

import "testing"

func TestValidateCheckAddress(t *testing.T) {
	cases := []struct {
		name string
		addr string
		ok   bool
	}{
		{"empty is allowed", "", true},
		{"host and port", "192.168.1.10:443", true},
		{"hostname and port", "server.lan:8096", true},
		{"missing port", "192.168.1.10", false},
		{"empty host", ":443", false},
		{"non-numeric port", "host:https", false},
		{"port out of range", "host:70000", false},
		{"port zero", "host:0", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateCheckAddress(c.addr)
			if c.ok && err != nil {
				t.Fatalf("want ok, got %v", err)
			}
			if !c.ok && err == nil {
				t.Fatalf("want error for %q", c.addr)
			}
		})
	}
}

func TestHostValidateCheckAddress(t *testing.T) {
	h := Host{Name: "n", Type: "vm", CheckAddress: "bad"}
	if err := h.Validate(); err == nil {
		t.Fatal("want error for malformed check address")
	}
	h.CheckAddress = "10.0.0.1:22"
	if err := h.Validate(); err != nil {
		t.Fatalf("valid check address rejected: %v", err)
	}
}
