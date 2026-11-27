package domain

import (
	"bytes"
	"encoding/binary"
	"net"
	"sort"
	"strings"
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
	FreeCount   int            // usable addresses not occupied by hosts, gateway or reservations
	Unbounded   bool           // subnet too large to count/enumerate (e.g. IPv6 /64)
	NextFree    string         // lowest usable address not assigned to any host ("" if none or Unbounded)

	ReservedCount int           // usable reserved/gateway addresses not already host-used (0 when Unbounded)
	Reservations  []Reservation // reservations for this network (for display)
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
func BuildIPAM(networks []Network, hosts []Host, reservations []Reservation) IPAMReport {
	used, unassigned := attribute(networks, hosts, -1)
	allocated := allocatedHostIPs(hosts)
	// Group once for the full report; rescanning every reservation for every
	// network makes otherwise small subnet reports quadratic in inventory size.
	byNetwork := make(map[int64][]Reservation, len(networks))
	for _, n := range networks {
		byNetwork[n.ID] = nil
	}
	for _, r := range reservations {
		if group, exists := byNetwork[r.NetworkID]; exists {
			byNetwork[r.NetworkID] = append(group, r)
		}
	}
	report := IPAMReport{Unassigned: sortAllocs(unassigned)}
	for i, n := range networks {
		report.Networks = append(report.Networks, buildUsage(n, sortAllocs(used[i]), byNetwork[n.ID], allocated))
	}
	return report
}

// BuildNetworkUsage derives the IP occupancy of a single network — the one
// being viewed — without running the per-network capacity/next-free
// enumeration for every other network (which can scan up to 2^16 addresses
// each). Attribution still runs across all networks so the longest-prefix rule
// matches BuildIPAM exactly: an IP that belongs to a more-specific subnet is
// not counted here. ok is false if no network with targetID exists.
func BuildNetworkUsage(targetID int64, networks []Network, hosts []Host, reservations []Reservation) (NetworkUsage, bool) {
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
	// Attribution still indexes every network (longest-prefix needs them all), but
	// only the target's allocations are collected — no slices are built for the
	// networks we are about to discard.
	used, _ := attribute(networks, hosts, targetIdx)
	return buildUsage(networks[targetIdx], sortAllocs(used[targetIdx]), reservationsFor(targetID, reservations), allocatedHostIPs(hosts)), true
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
// while still indexing all networks so the longest-prefix winner is unchanged.
func attribute(networks []Network, hosts []Host, onlyIdx int) (map[int][]Allocation, []Allocation) {
	index := newNetworkIndex(networks)

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
			best := index.lookup(ip)
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

// allocatedHostIPs keeps next-free suggestions safe across overlapping networks.
// Attribution and capacity remain local to the most-specific subnet, but an
// address used by a child subnet (or an equal-prefix winner) is not a free choice.
func allocatedHostIPs(hosts []Host) map[string]bool {
	allocated := make(map[string]bool)
	for _, host := range hosts {
		for _, raw := range host.IPs {
			if ip := net.ParseIP(raw); ip != nil {
				allocated[ip.String()] = true
			}
		}
	}
	return allocated
}

// reservationsFor returns the reservations that belong to network id.
func reservationsFor(id int64, reservations []Reservation) []Reservation {
	var out []Reservation
	for _, r := range reservations {
		if r.NetworkID == id {
			out = append(out, r)
		}
	}
	return out
}

// buildUsage computes the derived stats for one network from its (already
// IP-sorted) allocations.
func buildUsage(n Network, used []Allocation, reservations []Reservation, allocated map[string]bool) NetworkUsage {
	u := NetworkUsage{Network: n, Used: used, Reservations: reservations}

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
	case reservesEnds(ipn):
		u.TotalUsable = (1 << hostBits) - 2 // exclude network + broadcast
	default:
		u.TotalUsable = 1 << hostBits // /31, /32, and small IPv6 subnets
	}
	if !u.Unbounded {
		taken := make(map[string]bool, len(byIP)+1)
		for ip := range byIP {
			taken[ip] = true
		}
		// The gateway is a reserved address: never suggest it as free.
		if gw := net.ParseIP(strings.TrimSpace(n.Gateway)); gw != nil && ipn.Contains(gw) {
			taken[gw.String()] = true
		}
		// Count merged ranges instead of expanding every reserved address. Even
		// a /2 can then report accurate capacity using space proportional to
		// the number of reservations, not the size of the subnet.
		reserved := reservationRanges(ipn, reservations)
		reservedCount := 0
		for _, span := range reserved {
			reservedCount += int(binary.BigEndian.Uint32(span.end[12:])-binary.BigEndian.Uint32(span.start[12:])) + 1
		}
		// These addresses are already excluded from TotalUsable. Keep any host
		// allocations visible, but do not subtract subnet endpoints twice.
		if reservesEnds(ipn) {
			base := ipn.IP.Mask(ipn.Mask)
			broadcast := make(net.IP, len(base))
			for i := range base {
				broadcast[i] = base[i] | ^ipn.Mask[i]
			}
			delete(taken, base.String())
			delete(taken, broadcast.String())
		}
		for ip := range taken {
			if reserved.contains(net.ParseIP(ip)) {
				reservedCount-- // a host or gateway already accounts for it
			}
			if _, isUsed := byIP[ip]; !isUsed {
				u.ReservedCount++
			}
		}
		u.ReservedCount += reservedCount
		if u.FreeCount = u.TotalUsable - len(taken) - reservedCount; u.FreeCount < 0 {
			u.FreeCount = 0
		}
		u.NextFree = nextFree(ipn, taken, reserved, allocated)
	}
	return u
}

// reservesEnds reports whether a subnet reserves its network and broadcast
// addresses, so they count as neither usable capacity nor a next-free
// suggestion. Only IPv4 subnets larger than a /31 point-to-point link do,
// including their IPv6-mapped representation. /31, /32 and native IPv6 subnets
// use every address. This is shared by capacity, reservations and next-free.
func reservesEnds(ipn *net.IPNet) bool {
	ones, bits := ipn.Mask.Size()
	return ipn.IP.To4() != nil && bits-ones >= 2
}

// nextFree returns the lowest usable address in ipn absent from taken and all
// host allocations and outside reservations, or "" if no candidate exists or
// the subnet is larger than 2^maxEnumerableHostBits addresses. It
// walks addresses in place rather than materializing the whole range, so a large
// (but enumerable) subnet does not allocate one net.IP per address just to find
// the first hole.
func nextFree(ipn *net.IPNet, taken map[string]bool, reserved ipRanges, allocated map[string]bool) string {
	ones, bits := ipn.Mask.Size()
	if bits-ones > maxEnumerableHostBits {
		return ""
	}
	base := ipn.IP.Mask(ipn.Mask)
	cur := make(net.IP, len(base))
	copy(cur, base)

	excludeEnds := reservesEnds(ipn)
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
		if candidate := cur.String(); !taken[candidate] && !allocated[candidate] && !reserved.contains(cur) {
			return candidate
		}
		incIP(cur)
	}
	return ""
}

// ipRanges contains sorted, non-overlapping inclusive address ranges. All
// endpoints use 16 bytes so comparisons work for both IPv4 and IPv6.
type ipRange struct{ start, end net.IP }
type ipRanges []ipRange

func (ranges ipRanges) contains(ip net.IP) bool {
	ip = ip.To16()
	i := sort.Search(len(ranges), func(i int) bool {
		return bytes.Compare(ranges[i].end, ip) >= 0
	})
	return i < len(ranges) && bytes.Compare(ranges[i].start, ip) <= 0
}

// reservationRanges clips reservations to the usable part of a bounded subnet
// and merges overlaps. Counting each range needs only its low 32 bits because
// buildUsage calls this for subnets with at most 30 host bits.
func reservationRanges(ipn *net.IPNet, reservations []Reservation) ipRanges {
	base := ipn.IP.Mask(ipn.Mask)
	last := make(net.IP, len(base))
	for i := range base {
		last[i] = base[i] | ^ipn.Mask[i]
	}
	first, last := append(net.IP(nil), base.To16()...), last.To16()
	if reservesEnds(ipn) {
		incIP(first)
		// An IPv4 broadcast address always ends in at least two set bits.
		last[len(last)-1]--
	}
	var ranges ipRanges
	for _, r := range reservations {
		start := net.ParseIP(strings.TrimSpace(r.StartIP))
		end := net.ParseIP(strings.TrimSpace(r.EndIP))
		if start == nil || end == nil {
			continue
		}
		// Match the network's effective family, including IPv4-mapped IPv6
		// CIDRs whose 128-bit masks still contain ordinary IPv4 addresses.
		if (start.To4() == nil) != (base.To4() == nil) || (end.To4() == nil) != (base.To4() == nil) {
			continue
		}
		start, end = start.To16(), end.To16()
		if bytes.Compare(start, first) < 0 {
			start = first
		}
		if bytes.Compare(end, last) > 0 {
			end = last
		}
		if bytes.Compare(start, end) <= 0 {
			ranges = append(ranges, ipRange{start, end})
		}
	}
	sort.Slice(ranges, func(i, j int) bool {
		return bytes.Compare(ranges[i].start, ranges[j].start) < 0
	})
	merged := ranges[:0]
	for _, span := range ranges {
		if len(merged) == 0 || bytes.Compare(span.start, merged[len(merged)-1].end) > 0 {
			merged = append(merged, span)
		} else if bytes.Compare(span.end, merged[len(merged)-1].end) > 0 {
			merged[len(merged)-1].end = span.end
		}
	}
	return merged
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
