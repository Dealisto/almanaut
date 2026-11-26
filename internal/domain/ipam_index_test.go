package domain

import (
	"fmt"
	"math/rand/v2"
	"net"
	"reflect"
	"testing"
)

// linearAttribute is the original scan-based implementation. Keep this
// independent of networkIndex to check its semantics against net.IPNet.Contains.
func linearAttribute(networks []Network, hosts []Host, onlyIdx int) (map[int][]Allocation, []Allocation) {
	type parsedNet struct {
		idx      int
		ipn      *net.IPNet
		hostBits int
	}
	var parsed []parsedNet
	for i, n := range networks {
		_, ipn, err := net.ParseCIDR(n.CIDR)
		if err != nil {
			continue // invalid CIDR: this network matches nothing
		}
		ones, bits := ipn.Mask.Size()
		parsed = append(parsed, parsedNet{idx: i, ipn: ipn, hostBits: bits - ones})
	}

	used := make(map[int][]Allocation, len(networks))
	var unassigned []Allocation
	for _, h := range hosts {
		seen := make(map[string]bool, len(h.IPs))
		for _, raw := range h.IPs {
			ip := net.ParseIP(raw)
			if ip == nil {
				continue
			}
			canonical := ip.String()
			if seen[canonical] {
				continue // alternate spellings or repeats on one host are not conflicts
			}
			seen[canonical] = true
			best, bestHostBits := -1, 129
			for _, p := range parsed {
				// Compare block sizes so IPv4 /24 and mapped IPv6 /120 tie;
				// comparing raw prefix lengths would let /120 beat IPv4 /25.
				if p.ipn.Contains(ip) && p.hostBits < bestHostBits {
					best, bestHostBits = p.idx, p.hostBits
				}
			}
			if best < 0 {
				if onlyIdx < 0 {
					unassigned = append(unassigned, Allocation{IP: canonical, HostID: h.ID, HostName: h.Name})
				}
				continue
			}
			if onlyIdx < 0 || best == onlyIdx {
				used[best] = append(used[best], Allocation{IP: canonical, HostID: h.ID, HostName: h.Name})
			}
		}
	}
	return used, unassigned
}

func assertIndexedAttribution(t *testing.T, networks []Network, hosts []Host) {
	t.Helper()
	for _, only := range []int{-1, 0, len(networks) / 2, len(networks) - 1} {
		gotUsed, gotUnassigned := attribute(networks, hosts, only)
		wantUsed, wantUnassigned := linearAttribute(networks, hosts, only)
		if !reflect.DeepEqual(gotUsed, wantUsed) || !reflect.DeepEqual(gotUnassigned, wantUnassigned) {
			t.Fatalf("target %d: indexed allocations %v / %v differ from linear %v / %v", only, gotUsed, gotUnassigned, wantUsed, wantUnassigned)
		}
	}
}

func TestIndexedAttributionAddressFamilies(t *testing.T) {
	hosts := []Host{{ID: 1, Name: "mixed", IPs: []string{
		"192.0.2.1", "::ffff:192.0.2.1", "::ffff:c000:201",
		"::", "::1", "2001:db8::1", "ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff",
		"invalid", " 192.0.2.2", "fe80::1%eth0",
	}}}
	// IPv6 supernets that cover the mapped address range still must not match
	// IPv4 hosts. Exercise the /96 transition and every possible mask size.
	for bits := 0; bits <= 128; bits++ {
		networks := []Network{{ID: 1, CIDR: fmt.Sprintf("::ffff:192.0.2.1/%d", bits)}}
		t.Run(fmt.Sprint(bits), func(t *testing.T) {
			assertIndexedAttribution(t, networks, hosts)
		})
	}
	for bits := 0; bits <= 32; bits++ {
		networks := []Network{
			{ID: 1, CIDR: fmt.Sprintf("::ffff:192.0.2.1/%d", 96+bits)},
			{ID: 2, CIDR: fmt.Sprintf("192.0.2.1/%d", bits)},
			{ID: 3, CIDR: "::/0"},
		}
		assertIndexedAttribution(t, networks, hosts)
		networks[0], networks[1] = networks[1], networks[0]
		assertIndexedAttribution(t, networks, hosts)
	}
}

func TestIndexedAttributionMatchesLinearRandomized(t *testing.T) {
	for seed := uint64(0); seed < 40; seed++ {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			rng := rand.New(rand.NewPCG(seed, seed+1))
			networks := []Network{{ID: 1, CIDR: "invalid"}, {ID: 2, CIDR: "10.0.0.0/33"}, {ID: 3, CIDR: "2001:db8::/129"}}
			hosts := []Host{{ID: 1, IPs: []string{"invalid", "::1", "0.0.0.0", " 10.0.0.1", "fe80::1%eth0"}}}
			for i := 0; i < 100; i++ {
				var raw string
				var bits int
				switch rng.IntN(3) {
				case 0:
					raw = net.IPv4(byte(rng.IntN(256)), byte(rng.IntN(256)), byte(rng.IntN(256)), byte(rng.IntN(256))).String()
					bits = rng.IntN(33)
				case 1:
					raw = "::ffff:" + net.IPv4(byte(rng.IntN(256)), byte(rng.IntN(256)), byte(rng.IntN(256)), byte(rng.IntN(256))).String()
					bits = rng.IntN(129)
				default:
					ip := make(net.IP, net.IPv6len)
					for j := range ip {
						ip[j] = byte(rng.IntN(256))
					}
					raw = ip.String()
					bits = rng.IntN(129)
				}
				networks = append(networks, Network{ID: int64(len(networks) + 1), CIDR: fmt.Sprintf("%s/%d", raw, bits)})
				// Repeat spellings within a host must deduplicate; a second
				// host using that address must remain a distinct allocation.
				canonical := net.ParseIP(raw).String()
				hosts = append(hosts, Host{ID: int64(len(hosts) + 1), Name: fmt.Sprint(i), IPs: []string{raw, canonical, raw}})
				if i%7 == 0 {
					networks = append(networks, Network{ID: int64(len(networks) + 1), CIDR: networks[len(networks)-1].CIDR})
					hosts = append(hosts, Host{ID: int64(len(hosts) + 1), Name: "duplicate", IPs: []string{raw}})
				}
			}
			rng.Shuffle(len(networks), func(i, j int) { networks[i], networks[j] = networks[j], networks[i] })
			assertIndexedAttribution(t, networks, hosts)
		})
	}
}

func TestIPAMNextFreeAtMaximumAddress(t *testing.T) {
	for _, cidr := range []string{"255.255.255.255/32", "::ffff:255.255.255.255/128", "ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff/128"} {
		t.Run(cidr, func(t *testing.T) {
			ip, _, err := net.ParseCIDR(cidr)
			if err != nil {
				t.Fatal(err)
			}
			network := Network{ID: 1, CIDR: cidr}
			empty := BuildIPAM([]Network{network}, nil, nil).Networks[0]
			if empty.NextFree != ip.String() {
				t.Fatalf("empty network: %+v", empty)
			}
			hosts := []Host{{ID: 1, IPs: []string{ip.String()}}}
			full := BuildIPAM([]Network{network}, hosts, nil).Networks[0]
			if full.NextFree != "" || full.FreeCount != 0 || full.UsedCount != 1 {
				t.Fatalf("full network: %+v", full)
			}
			single, ok := BuildNetworkUsage(network.ID, []Network{network}, hosts, nil)
			if !ok || !reflect.DeepEqual(single, full) {
				t.Fatalf("single network: %+v", single)
			}
		})
	}
}
