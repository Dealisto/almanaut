package web

import "testing"

func TestMatchesQuery(t *testing.T) {
	fields := []string{"proxmox", "Ubuntu 24.04", "runs the media stack", "10.0.0.5"}

	cases := map[string]bool{
		"proxmox": true,  // name
		"media":   true,  // note substring
		"10.0.0":  true,  // ip substring
		"UBUNTU":  true,  // case-insensitive
		"zzz":     false, // no match
	}
	for q, want := range cases {
		if got := matchesQuery(fields, q); got != want {
			t.Errorf("matchesQuery(_, %q) = %v, want %v", q, got, want)
		}
	}
}
