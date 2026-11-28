package agent

import (
	"reflect"
	"testing"

	"github.com/Dealisto/almanaut/internal/agentapi"
)

func fakeLister(ifaces ...NetInterface) InterfaceLister {
	return func() ([]NetInterface, error) { return ifaces, nil }
}

func TestCollectInterfacesDropsLoopbackAndDockerNoise(t *testing.T) {
	got := CollectInterfaces(fakeLister(
		NetInterface{Name: "lo", MAC: "", Addrs: []string{"127.0.0.1/8"}},
		NetInterface{Name: "eth0", MAC: "aa:bb:cc:00:00:01", Addrs: []string{"192.168.1.10/24"}},
		NetInterface{Name: "docker0", MAC: "02:42:ac:11:00:01", Addrs: []string{"172.17.0.1/16"}},
		NetInterface{Name: "br-1a2b3c", MAC: "02:42:ac:12:00:01", Addrs: []string{"172.18.0.1/16"}},
		NetInterface{Name: "veth9f2a", MAC: "aa:11:22:33:44:55", Addrs: nil},
	))
	if len(got) != 1 {
		t.Fatalf("got %d interfaces (%+v), want only eth0", len(got), got)
	}
	if got[0].Name != "eth0" || got[0].MAC != "aa:bb:cc:00:00:01" {
		t.Fatalf("interface = %+v", got[0])
	}
}

// Tailscale is deliberately kept: it is frequently the only address by which
// the machine can actually be reached.
func TestCollectInterfacesKeepsTailscale(t *testing.T) {
	got := CollectInterfaces(fakeLister(
		NetInterface{Name: "tailscale0", MAC: "", Addrs: []string{"100.101.102.103/32"}},
	))
	if len(got) != 1 || got[0].Name != "tailscale0" {
		t.Fatalf("got %+v, want tailscale0 kept", got)
	}
}

func TestPodmanBridgesDoNotPolluteHostIdentityOrAdoption(t *testing.T) {
	report := agentapi.Report{Hostname: "container-host", Interfaces: CollectInterfaces(fakeLister(
		NetInterface{Name: "eth0", MAC: "02:00:00:00:00:01", Addrs: []string{"192.0.2.10/24"}},
		NetInterface{Name: "podman0", MAC: "02:00:00:00:00:02", Addrs: []string{"10.88.0.1/16"}},
		NetInterface{Name: "podman13", MAC: "02:00:00:00:00:03", Addrs: []string{"10.89.12.1/24"}},
		NetInterface{Name: "cni-podman0", MAC: "02:00:00:00:00:04", Addrs: []string{"10.90.0.1/16"}},
	))}
	if got := report.ReportedIPs(); !reflect.DeepEqual(got, []string{"192.0.2.10"}) {
		t.Errorf("container bridge addresses leaked into host inventory: %v", got)
	}
	if got := report.Fingerprint(); got != "container-host|02:00:00:00:00:01" {
		t.Errorf("container bridge MACs leaked into host identity: %q", got)
	}
	// Separate Podman hosts commonly reuse these private bridge addresses.
	// They must not turn the real host match into an ambiguous adoption.
	decision := agentapi.Resolve(report, nil, []agentapi.HostRef{
		{ID: 1, Name: "container-host", IPs: []string{"192.0.2.10"}},
		{ID: 2, Name: "another-host", IPs: []string{"10.88.0.1"}},
	})
	if decision.Kind != agentapi.DecideAdopt || decision.HostID != 1 {
		t.Fatalf("Podman bridge caused incorrect adoption: %+v", decision)
	}
}

// Sanitisation belongs to agentapi.ReportedIPs, which the server shares — the
// agent must not pre-filter and risk the two definitions drifting apart.
func TestCollectInterfacesPassesAddressesThroughUnmodified(t *testing.T) {
	got := CollectInterfaces(fakeLister(
		NetInterface{Name: "eth0", MAC: "aa:bb:cc:00:00:01", Addrs: []string{"192.168.1.10/24", "fe80::1%eth0"}},
	))
	if len(got) != 1 || len(got[0].Addrs) != 2 {
		t.Fatalf("got %+v, want both addresses passed through", got)
	}
	if got[0].Addrs[0] != "192.168.1.10/24" {
		t.Fatalf("address was rewritten: %q", got[0].Addrs[0])
	}
}

func TestCollectInterfacesToleratesListerError(t *testing.T) {
	got := CollectInterfaces(func() ([]NetInterface, error) { return nil, errFake })
	if len(got) != 0 {
		t.Fatalf("got %+v, want empty on error", got)
	}
}

func TestCollectInterfacesToleratesNilLister(t *testing.T) {
	got := CollectInterfaces(nil)
	if len(got) != 0 {
		t.Fatalf("got %+v, want empty on nil lister, not panic", got)
	}
}

var errFake = errTest("boom")

type errTest string

func (e errTest) Error() string { return string(e) }
