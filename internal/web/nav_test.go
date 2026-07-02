package web

import "testing"

func TestNavIsActive(t *testing.T) {
	cases := []struct {
		path, base string
		want       bool
	}{
		{"/", "/", true},
		{"/hosts", "/", false},
		{"/hosts", "/hosts", true},
		{"/hosts/5", "/hosts", true},
		{"/hosts/new", "/hosts", true},
		{"/hardware", "/hosts", false},
		{"/host", "/hosts", false},   // shared-prefix guard
		{"/hostsx", "/hosts", false}, // shared-prefix guard
		{"/discovery/docker", "/discovery", true},
	}
	for _, c := range cases {
		if got := navIsActive(c.path, c.base); got != c.want {
			t.Errorf("navIsActive(%q, %q) = %v, want %v", c.path, c.base, got, c.want)
		}
	}
}
