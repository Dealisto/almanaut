package domain

import (
	"fmt"
	"net/netip"
	"strings"
)

var IPAddressStatuses = []string{"active", "reserved", "deprecated"}

// IPAddress is an explicit allocation, optionally assigned to an interface.
// Address retains host bits (192.0.2.10/24 is not the network 192.0.2.0/24).
type IPAddress struct {
	ID          int64  `json:"id" yaml:"id"`
	Address     string `json:"address" yaml:"address"`
	InterfaceID int64  `json:"interface_id" yaml:"interface_id"`
	Status      string `json:"status" yaml:"status"`
	DNSName     string `json:"dns_name" yaml:"dns_name"`
	Notes       string `json:"notes" yaml:"notes"`
}

func (v IPAddress) Validate() error {
	if _, err := ParseInterfaceAddress(v.Address); err != nil {
		return err
	}
	if v.InterfaceID < 0 {
		return fmt.Errorf("invalid interface reference")
	}
	if !contains(IPAddressStatuses, v.Status) {
		return fmt.Errorf("invalid address status %q", v.Status)
	}
	if strings.ContainsAny(v.DNSName, "\r\n\t ") {
		return fmt.Errorf("DNS name cannot contain whitespace")
	}
	return nil
}

// ParseInterfaceAddress canonicalizes IPv4 and IPv6 while preserving host bits.
func ParseInterfaceAddress(raw string) (netip.Prefix, error) {
	raw = strings.TrimSpace(raw)
	p, err := netip.ParsePrefix(raw)
	if err != nil {
		a, e := netip.ParseAddr(raw)
		if e != nil || a.Zone() != "" {
			return netip.Prefix{}, fmt.Errorf("invalid IP address or prefix %q", raw)
		}
		p = netip.PrefixFrom(a, a.BitLen())
	}
	if p.Addr().Is4In6() {
		return netip.Prefix{}, fmt.Errorf("use native IPv4 instead of an IPv4-mapped IPv6 address")
	}
	return p, nil
}
