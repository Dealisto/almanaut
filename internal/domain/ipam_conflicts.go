package domain

import (
	"bytes"
	"net"
	"sort"
	"strconv"
)

// This file adds IPAM consistency checks on top of the occupancy report in
// ipam.go. Everything here is a pure projection of networks and hosts, reusing
// the same attribution helper so the "outside every network" list matches the
// occupancy report's Unassigned list exactly.

// IPConflict is one IP address claimed by two or more distinct hosts.
type IPConflict struct {
	IP    string
	Hosts []Allocation // one entry per host, sorted by host name
}

// NetworkOverlap is a pair of networks whose CIDRs occupy the same block. A
// strict parent/child (one prefix inside a broader one) is legitimate and is
// not reported.
type NetworkOverlap struct {
	A Network
	B Network
}

// IPAMConflicts groups the three IPAM inconsistency classes.
type IPAMConflicts struct {
	DuplicateIPs    []IPConflict     // same IP on two+ hosts
	OutsideNetworks []Allocation     // host IPs in no known network
	Overlaps        []NetworkOverlap // networks sharing a block
}

// Any reports whether any conflict of any class was found.
func (c IPAMConflicts) Any() bool {
	return len(c.DuplicateIPs) > 0 || len(c.OutsideNetworks) > 0 || len(c.Overlaps) > 0
}

// BuildIPAMConflicts derives the IPAM inconsistencies from networks and hosts.
// It is a pure projection of its inputs and does not touch the database.
func BuildIPAMConflicts(networks []Network, hosts []Host) IPAMConflicts {
	return IPAMConflicts{
		DuplicateIPs:    duplicateIPs(hosts),
		OutsideNetworks: outsideNetworks(networks, hosts),
		Overlaps:        overlappingNetworks(networks),
	}
}

// duplicateIPs groups host IPs by their canonical form and returns those claimed
// by more than one host, sorted by IP. A host listing the same IP twice counts
// once. Unparseable IPs are skipped.
func duplicateIPs(hosts []Host) []IPConflict {
	byIP := map[string][]Allocation{}
	seen := map[string]bool{} // "ip|hostID" so one host's repeat does not self-collide
	for _, h := range hosts {
		for _, raw := range h.IPs {
			ip := net.ParseIP(raw)
			if ip == nil {
				continue
			}
			s := ip.String()
			key := s + "|" + strconv.FormatInt(h.ID, 10)
			if seen[key] {
				continue
			}
			seen[key] = true
			byIP[s] = append(byIP[s], Allocation{IP: s, HostID: h.ID, HostName: h.Name})
		}
	}
	var out []IPConflict
	for ip, allocs := range byIP {
		if len(allocs) >= 2 {
			out = append(out, IPConflict{IP: ip, Hosts: sortAllocs(allocs)})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return ipLess(out[i].IP, out[j].IP)
	})
	return out
}

// outsideNetworks returns the host IPs that fall in no known network, reusing
// the shared attribution helper so the result matches BuildIPAM's Unassigned
// list exactly.
func outsideNetworks(networks []Network, hosts []Host) []Allocation {
	_, unassigned := attribute(networks, hosts, -1)
	return sortAllocs(unassigned)
}

// overlappingNetworks returns every pair of networks that occupy the same CIDR
// block. Aligned CIDR blocks can only ever overlap by containment, so a pair
// with different prefix lengths is a legitimate parent/child and is excluded;
// the remaining overlap is two networks sharing the identical block.
func overlappingNetworks(networks []Network) []NetworkOverlap {
	type parsed struct {
		n          Network
		block      networkBlock
		groupIndex int
	}
	var nets []parsed
	groups := make(map[networkBlock][]Network, len(networks))
	for _, n := range networks {
		if _, ipn, err := net.ParseCIDR(n.CIDR); err == nil {
			block := blockOf(ipn)
			nets = append(nets, parsed{n, block, len(groups[block])})
			groups[block] = append(groups[block], n)
		}
	}
	var out []NetworkOverlap
	// Only compare members of an identical block. This is linear for distinct
	// subnets and preserves input pair ordering when duplicate blocks occur.
	for _, n := range nets {
		for _, peer := range groups[n.block][n.groupIndex+1:] {
			out = append(out, NetworkOverlap{A: n.n, B: peer})
		}
	}
	return out
}

// NetworkOverlaps returns the other networks that occupy the same block as
// target — the per-network view of overlappingNetworks, used by the network
// detail page.
func NetworkOverlaps(target Network, networks []Network) []Network {
	_, tipn, err := net.ParseCIDR(target.CIDR)
	if err != nil {
		return nil
	}
	var out []Network
	for _, n := range networks {
		if n.ID == target.ID {
			continue
		}
		if _, ipn, err := net.ParseCIDR(n.CIDR); err == nil && sameBlock(tipn, ipn) {
			out = append(out, n)
		}
	}
	return out
}

// sameBlock reports whether two aligned CIDR blocks are identical (same size
// and same masked base). Blocks of different sizes are parent/child, not
// a conflict.
func sameBlock(a, b *net.IPNet) bool {
	return blockOf(a) == blockOf(b)
}

type networkBlock struct {
	base     string
	hostBits int
}

func blockOf(ipn *net.IPNet) networkBlock {
	ones, bits := ipn.Mask.Size()
	// IP.String canonicalizes IPv4 and its IPv6-mapped spelling alike.
	// Compare block sizes on that same basis (/24 equals mapped /120).
	return networkBlock{base: ipn.IP.String(), hostBits: bits - ones}
}

// ipLess orders two IP strings by parsed value, falling back to lexical order
// for anything unparseable.
func ipLess(a, b string) bool {
	ia, ib := net.ParseIP(a), net.ParseIP(b)
	if ia == nil || ib == nil {
		return a < b
	}
	return bytes.Compare(ia.To16(), ib.To16()) < 0
}
