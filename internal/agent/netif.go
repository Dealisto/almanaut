package agent

import (
	"net"
	"strings"

	"github.com/Dealisto/almanaut/internal/agentapi"
)

// NetInterface is one interface as the agent sees it, independent of the net
// package so tests can supply one without a real network stack.
type NetInterface struct {
	Name  string
	MAC   string
	Addrs []string
}

// InterfaceLister enumerates interfaces. net.Interfaces() cannot be pointed at
// a fixture tree, so this function type is the seam tests substitute.
type InterfaceLister func() ([]NetInterface, error)

// noisyPrefixes are interfaces that describe container plumbing rather than
// the machine. tailscale is deliberately absent: that address is often the
// only way the machine is actually reachable.
var noisyPrefixes = []string{"docker", "br-", "veth", "virbr", "cni", "flannel", "kube-", "cali"}

// SystemInterfaces lists the real machine's interfaces.
func SystemInterfaces() ([]NetInterface, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	out := make([]NetInterface, 0, len(ifaces))
	for _, i := range ifaces {
		ni := NetInterface{Name: i.Name, MAC: i.HardwareAddr.String()}
		if i.Flags&net.FlagLoopback != 0 {
			ni.Name = "lo" // normalized so the filter below catches it everywhere
		}
		addrs, err := i.Addrs()
		if err == nil {
			for _, a := range addrs {
				ni.Addrs = append(ni.Addrs, a.String())
			}
		}
		out = append(out, ni)
	}
	return out, nil
}

// CollectInterfaces converts and filters interfaces for the report.
//
// Addresses are passed through untouched: agentapi.ReportedIPs already strips
// CIDR suffixes and IPv6 zones and drops loopback and link-local, and it is
// shared with the server. Filtering here too would create a second definition
// that could drift from it.
//
// A lister error yields no interfaces rather than an error, because an empty
// list is the wire signal for "could not determine" and the server then keeps
// the host's existing addresses.
func CollectInterfaces(list InterfaceLister) []agentapi.Interface {
	ifaces, err := list()
	if err != nil {
		return nil
	}
	out := make([]agentapi.Interface, 0, len(ifaces))
	for _, i := range ifaces {
		if i.Name == "lo" || isNoisyInterface(i.Name) {
			continue
		}
		out = append(out, agentapi.Interface{Name: i.Name, MAC: i.MAC, Addrs: i.Addrs})
	}
	return out
}

func isNoisyInterface(name string) bool {
	for _, p := range noisyPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}
