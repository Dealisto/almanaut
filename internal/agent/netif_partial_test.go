package agent

import (
	"net"
	"reflect"
	"testing"

	"github.com/Dealisto/almanaut/internal/agentapi"
)

func TestInterfaceAddressFailurePreservesUnknownInsteadOfPartialIPs(t *testing.T) {
	for _, failed := range []string{"eth0", "eth1"} {
		t.Run(failed, func(t *testing.T) {
			interfaces := []net.Interface{
				{Name: "eth0", HardwareAddr: net.HardwareAddr{0x02, 0, 0, 0, 0, 1}},
				{Name: "eth1", HardwareAddr: net.HardwareAddr{0x02, 0, 0, 0, 0, 2}},
			}
			addresses := func(i *net.Interface) ([]net.Addr, error) {
				if i.Name == failed {
					return nil, errFake
				}
				return []net.Addr{&net.IPNet{IP: net.ParseIP("192.0.2.1"), Mask: net.CIDRMask(24, 32)}}, nil
			}
			list := func() ([]NetInterface, error) { return systemInterfaces(interfaces, addresses), nil }
			report := Collect(CollectOptions{Root: fixtureRoot(t, nil), Hostname: "router", Interfaces: list})
			if ips := report.ReportedIPs(); len(ips) != 0 {
				t.Fatalf("partial IP set would overwrite the host's complete inventory: %v", ips)
			}
			if len(report.Interfaces) != 2 || report.Fingerprint() != "router|02:00:00:00:00:01,02:00:00:00:00:02" {
				t.Fatalf("address failure discarded known identity facts: %+v", report.Interfaces)
			}
		})
	}
}

func TestInterfaceAddressFailureOnExcludedInterfacesDoesNotLoseHostIPs(t *testing.T) {
	interfaces := []net.Interface{
		{Name: "loopback-alt-name", Flags: net.FlagLoopback},
		{Name: "docker0"},
		{Name: "veth123"},
		{Name: "eth0"},
		{Name: "tailscale0"},
	}
	addresses := func(i *net.Interface) ([]net.Addr, error) {
		var raw string
		switch i.Name {
		case "eth0":
			raw = "192.0.2.1/24"
		case "tailscale0":
			raw = "100.64.0.1/32"
		default:
			return nil, errFake
		}
		ip, prefix, err := net.ParseCIDR(raw)
		if err != nil {
			t.Fatal(err)
		}
		prefix.IP = ip
		return []net.Addr{prefix}, nil
	}
	report := agentapi.Report{Interfaces: CollectInterfaces(func() ([]NetInterface, error) {
		return systemInterfaces(interfaces, addresses), nil
	})}
	if got, want := report.ReportedIPs(), []string{"192.0.2.1", "100.64.0.1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unrelated interface failure lost usable addresses: %v, want %v", got, want)
	}
}
