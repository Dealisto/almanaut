package domain

import (
	"bytes"
	"net"
	"sort"
)

// maxEnumerableHostBits caps the subnet size NextFree will scan: 2^16 addresses.
const maxEnumerableHostBits = 16

// Allocation is one host IP and the host that claims it.
type Allocation struct {
	IP       string
	HostID   int64
	HostName string
}

// NetworkUsage is the derived IP occupancy of a single network.
type NetworkUsage struct {
	Network     Network
	Used        []Allocation   // sorted by IP; a conflicting IP appears once per host
	Conflicts   [][]Allocation // groups of >=2 allocations sharing one IP
	TotalUsable int            // usable addresses in the subnet (0 when Unbounded)
	UsedCount   int            // distinct IPs in use
	FreeCount   int            // TotalUsable - UsedCount, clamped at 0 (0 when Unbounded)
	Unbounded   bool           // subnet too large to count/enumerate (e.g. IPv6 /64)
	NextFree    string         // lowest usable address not in use ("" if none or Unbounded)
}

// IPAMReport is the IP occupancy across all networks.
type IPAMReport struct {
	Networks   []NetworkUsage
	Unassigned []Allocation // host IPs that fall in no known network
}

// BuildIPAM derives per-network IP occupancy from networks and hosts. It is a
// pure projection of its inputs and does not touch the database. Each host IP is
// attributed to the network whose CIDR contains it with the longest prefix
// (most specific), mirroring routing; IPs in no known network are unassigned.
func BuildIPAM(networks []Network, hosts []Host) IPAMReport {
	used, unassigned := attribute(networks, hosts, -1)
	report := IPAMReport{Unassigned: sortAllocs(unassigned)}
	for i, n := range networks {
		report.Networks = append(report.Networks, buildUsage(n, sortAllocs(used[i])))
	}
	return report
}

// BuildNetworkUsage derives the IP occupancy of a single network — the one
// being viewed — without running the per-network capacity/next-free
// enumeration for every other network (which can scan up to 2^16 addresses
// each). Attribution still runs across all networks so the longest-prefix rule
// matches BuildIPAM exactly: an IP that belongs to a more-specific subnet is
// not counted here. ok is false if no network with targetID exists.
func BuildNetworkUsage(targetID int64, networks []Network, hosts []Host) (NetworkUsage, bool) {
	targetIdx := -1
	for i, n := range networks {
		if n.ID == targetID {
			targetIdx = i
			break
		}
	}
	if targetIdx < 0 {
		return NetworkUsage{}, false
	}
	// Attribution still scans every network (longest-prefix needs them all), but
	// only the target's allocations are collected — no slices are built for the
	// networks we are about to discard.
	used, _ := attribute(networks, hosts, targetIdx)
	return buildUsage(networks[targetIdx], sortAllocs(used[targetIdx])), true
}

// attribute assigns each host IP to the index of the network containing it with
// the longest prefix (most specific), returning the per-network allocations and
// the IPs that fall in no known network. It is the shared core of BuildIPAM and
// BuildNetworkUsage so both apply identical longest-prefix and tie-breaking
// rules.
//
// onlyIdx limits what is collected: -1 keeps every network's allocations plus
// the unassigned list (the full report); a non-negative value keeps only that
// network's allocations (the single-network view) and skips the unassigned list,
// while still scanning all networks so the longest-prefix winner is unchanged.
func attribute(networks []Network, hosts []Host, onlyIdx int) (map[int][]Allocation, []Allocation) {
	type parsedNet struct {
		idx  int
		ipn  *net.IPNet
		ones int
	}
	var parsed []parsedNet
	for i, n := range networks {
		_, ipn, err := net.ParseCIDR(n.CIDR)
		if err != nil {
			continue // invalid CIDR: this network matches nothing
		}
		ones, _ := ipn.Mask.Size()
		parsed = append(parsed, parsedNet{idx: i, ipn: ipn, ones: ones})
	}

	used := make(map[int][]Allocation, len(networks))
	var unassigned []Allocation
	for _, h := range hosts {
		for _, raw := range h.IPs {
			ip := net.ParseIP(raw)
			if ip == nil {
				continue
			}
			best, bestOnes := -1, -1
			for _, p := range parsed {
				if p.ipn.Contains(ip) && p.ones > bestOnes {
					best, bestOnes = p.idx, p.ones
				}
			}
			if best < 0 {
				if onlyIdx < 0 {
					unassigned = append(unassigned, Allocation{IP: ip.String(), HostID: h.ID, HostName: h.Name})
				}
				continue
			}
			if onlyIdx < 0 || best == onlyIdx {
				used[best] = append(used[best], Allocation{IP: ip.String(), HostID: h.ID, HostName: h.Name})
			}
		}
	}
	return used, unassigned
}

// buildUsage computes the derived stats for one network from its (already
// IP-sorted) allocations.
func buildUsage(n Network, used []Allocation) NetworkUsage {
	u := NetworkUsage{Network: n, Used: used}

	byIP := map[string][]Allocation{}
	var order []string
	for _, a := range used {
		if _, ok := byIP[a.IP]; !ok {
			order = append(order, a.IP)
		}
		byIP[a.IP] = append(byIP[a.IP], a)
	}
	u.UsedCount = len(order)
	for _, ip := range order {
		if g := byIP[ip]; len(g) >= 2 {
			u.Conflicts = append(u.Conflicts, g)
		}
	}

	_, ipn, err := net.ParseCIDR(n.CIDR)
	if err != nil {
		return u // invalid CIDR: no capacity math
	}
	ones, bits := ipn.Mask.Size()
	hostBits := bits - ones
	switch {
	case hostBits >= 31:
		u.Unbounded = true
	case bits == 32 && hostBits >= 2:
		u.TotalUsable = (1 << hostBits) - 2 // exclude network + broadcast
	default:
		u.TotalUsable = 1 << hostBits // /31, /32, and small IPv6 subnets
	}
	if !u.Unbounded {
		if u.FreeCount = u.TotalUsable - u.UsedCount; u.FreeCount < 0 {
			u.FreeCount = 0
		}
		taken := make(map[string]bool, len(byIP)+1)
		for ip := range byIP {
			taken[ip] = true
		}
		// The gateway is a reserved address: never suggest it as free.
		if gw := net.ParseIP(n.Gateway); gw != nil && ipn.Contains(gw) {
			taken[gw.String()] = true
		}
		u.NextFree = nextFree(ipn, taken)
	}
	return u
}

// nextFree returns the lowest usable address in ipn not present in taken, or ""
// if the subnet is full or larger than 2^maxEnumerableHostBits addresses. It
// walks addresses in place rather than materializing the whole range, so a large
// (but enumerable) subnet does not allocate one net.IP per address just to find
// the first hole.
func nextFree(ipn *net.IPNet, taken map[string]bool) string {
	ones, bits := ipn.Mask.Size()
	if bits-ones > maxEnumerableHostBits {
		return ""
	}
	base := ipn.IP.Mask(ipn.Mask)
	cur := make(net.IP, len(base))
	copy(cur, base)

	// IPv4 subnets larger than a point-to-point link reserve the network and
	// broadcast addresses; /31, /32 and IPv6 subnets use every address.
	excludeEnds := bits == 32 && bits-ones >= 2
	var broadcast net.IP
	if excludeEnds {
		broadcast = make(net.IP, len(base))
		for i := range base {
			broadcast[i] = base[i] | ^ipn.Mask[i]
		}
		incIP(cur) // skip the network address
	}

	for ipn.Contains(cur) {
		if excludeEnds && cur.Equal(broadcast) {
			break // reached the reserved broadcast address
		}
		if !taken[cur.String()] {
			return cur.String()
		}
		incIP(cur)
	}
	return ""
}

// incIP increments an IP address in place (big-endian).
func incIP(ip net.IP) {
	for i := len(ip) - 1; i >= 0; i-- {
		ip[i]++
		if ip[i] != 0 {
			break
		}
	}
}

// sortAllocs orders allocations by parsed IP value (so .2 precedes .10), then by
// host name for a stable order among IPs that collide.
func sortAllocs(a []Allocation) []Allocation {
	sort.SliceStable(a, func(i, j int) bool {
		ii, ij := net.ParseIP(a[i].IP), net.ParseIP(a[j].IP)
		if ii == nil || ij == nil {
			return a[i].IP < a[j].IP
		}
		if c := bytes.Compare(ii.To16(), ij.To16()); c != 0 {
			return c < 0
		}
		return a[i].HostName < a[j].HostName
	})
	return a
}
