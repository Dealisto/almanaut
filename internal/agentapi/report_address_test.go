package agentapi

import "testing"

func TestMalformedReportedAddressesCannotAdoptAnotherHost(t *testing.T) {
	for _, address := range []string{
		"192.0.2.1/garbage", "192.0.2.1/33", "192.0.2.1/", "192.0.2.1/24/extra",
		"192.0.2.1%eth0", "192.0.2.1%", "2001:db8::1/129", "2001:db8::1/garbage",
		"2001:db8::1%/64",
	} {
		t.Run(address, func(t *testing.T) {
			r := Report{Hostname: "new-host", Interfaces: []Interface{{Addrs: []string{address}}}}
			if ips := r.ReportedIPs(); len(ips) != 0 {
				t.Fatalf("malformed address produced host IPs: %v", ips)
			}
			hosts := []HostRef{{ID: 9, Name: "existing-host", IPs: []string{"192.0.2.1", "2001:db8::1"}}}
			if got := Resolve(r, nil, hosts); got.Kind != DecideCreate {
				t.Fatalf("malformed address must not select existing inventory: %+v", got)
			}
		})
	}
}

func TestReportedAddressesKeepValidPrefixesAndIPv6Zones(t *testing.T) {
	for _, tc := range []struct{ address, want string }{
		{"192.0.2.1/0", "192.0.2.1"},
		{"192.0.2.1/32", "192.0.2.1"},
		{"2001:db8::1%eth0", "2001:db8::1"},
		{"2001:db8::1%eth0/64", "2001:db8::1"},
		{"2001:db8::1/128", "2001:db8::1"},
		{"::ffff:192.0.2.1/120", "192.0.2.1"},
	} {
		t.Run(tc.address, func(t *testing.T) {
			r := Report{Interfaces: []Interface{{Addrs: []string{tc.address}}}}
			if got := r.ReportedIPs(); len(got) != 1 || got[0] != tc.want {
				t.Fatalf("valid address = %v, want [%s]", got, tc.want)
			}
		})
	}
}
