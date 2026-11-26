package domain

import (
	"net"
	"net/netip"
)

// networkIndex groups canonical prefixes by address family. A lookup checks
// only the prefix lengths present, longest first: at most 33 IPv4 or 129 IPv6
// lengths regardless of the number of networks in the inventory.
type networkIndex struct {
	prefixes  map[netip.Prefix]int
	v4Lengths []int
	v6Lengths []int
}

func newNetworkIndex(networks []Network) networkIndex {
	index := networkIndex{prefixes: make(map[netip.Prefix]int, len(networks))}
	var v4Lengths [33]bool
	var v6Lengths [129]bool
	for i, network := range networks {
		_, subnet, err := net.ParseCIDR(network.CIDR)
		if err != nil {
			continue
		}
		address, _ := netip.AddrFromSlice(subnet.IP)
		bits, width := subnet.Mask.Size()
		address = address.Unmap()
		if address.Is4() {
			// net.IPNet.Contains treats mapped IPv6 networks as IPv4 and
			// uses the low 32 mask bits. A mapped /120 therefore equals /24.
			bits -= width - 32
			v4Lengths[bits] = true
		} else {
			v6Lengths[bits] = true
		}
		prefix := netip.PrefixFrom(address, bits).Masked()
		if _, exists := index.prefixes[prefix]; !exists {
			index.prefixes[prefix] = i // first input wins equal-prefix ties
		}
	}
	for bits := len(v4Lengths) - 1; bits >= 0; bits-- {
		if v4Lengths[bits] {
			index.v4Lengths = append(index.v4Lengths, bits)
		}
	}
	for bits := len(v6Lengths) - 1; bits >= 0; bits-- {
		if v6Lengths[bits] {
			index.v6Lengths = append(index.v6Lengths, bits)
		}
	}
	return index
}

func (index networkIndex) lookup(ip net.IP) int {
	address, ok := netip.AddrFromSlice(ip)
	if !ok {
		return -1
	}
	address = address.Unmap()
	lengths := index.v6Lengths
	if address.Is4() {
		lengths = index.v4Lengths
	}
	// Match net.IPNet.Contains: a native IPv6 supernet such as ::/0 does
	// not contain IPv4 addresses, even when they use a mapped spelling.
	for _, bits := range lengths {
		if idx, found := index.prefixes[netip.PrefixFrom(address, bits).Masked()]; found {
			return idx
		}
	}
	return -1
}
