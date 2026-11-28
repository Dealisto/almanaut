package domain

import "testing"

func TestInterfaceAddressesPreserveHostBits(t *testing.T) {
	for _, tc := range []struct{ input, want string }{{"192.0.2.10/24", "192.0.2.10/24"}, {"192.0.2.10", "192.0.2.10/32"}, {"2001:0DB8::10/64", "2001:db8::10/64"}, {"::1", "::1/128"}} {
		got, err := ParseInterfaceAddress(tc.input)
		if err != nil || got.String() != tc.want {
			t.Errorf("%s: %v %v", tc.input, got, err)
		}
	}
	for _, bad := range []string{"not-an-ip", "192.0.2.1/33", "fe80::1%eth0", "::ffff:192.0.2.1/120"} {
		if _, err := ParseInterfaceAddress(bad); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}
