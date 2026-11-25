package agentapi

import "testing"

func TestResolveRejectsAmbiguousCandidatesInAnyOrder(t *testing.T) {
	r := Report{Hostname: "nas", Interfaces: []Interface{{Addrs: []string{"192.0.2.1", "2001:db8::1"}}}}
	for _, hosts := range [][]HostRef{
		{{ID: 1, Name: "nas"}, {ID: 2, Name: " NAS "}},
		{{ID: 1, Name: "old", IPs: []string{"192.0.2.1"}}, {ID: 2, Name: "other", IPs: []string{"::ffff:192.0.2.1"}}},
		{{ID: 1, Name: "nas"}, {ID: 2, Name: "other", IPs: []string{"2001:db8::1"}}},
		{{ID: 1, Name: "first", IPs: []string{"192.0.2.1"}}, {ID: 2, Name: "second", IPs: []string{"2001:db8::1"}}},
	} {
		for range 2 {
			if got := Resolve(r, nil, hosts); got.Kind != DecideAmbiguous || got.HostID != 0 {
				t.Fatalf("ambiguous %v resolved to %+v", hosts, got)
			}
			hosts[0], hosts[1] = hosts[1], hosts[0]
		}
		if got := Resolve(r, binding(42, r), hosts); got.Kind != DecideUpdate || got.HostID != 42 {
			t.Fatalf("established identity must take precedence: %+v", got)
		}
	}
}

func TestResolveMultipleSignalsForOneHostAreNotAmbiguous(t *testing.T) {
	r := Report{Hostname: "nas", Interfaces: []Interface{{Addrs: []string{"192.0.2.1", "2001:db8::1"}}}}
	host := HostRef{ID: 7, Name: "NAS", IPs: []string{"192.0.2.1", "2001:db8::1"}}
	if got := Resolve(r, nil, []HostRef{host, host}); got.Kind != DecideAdopt || got.HostID != 7 {
		t.Fatalf("one candidate with several matching signals: %+v", got)
	}
}
