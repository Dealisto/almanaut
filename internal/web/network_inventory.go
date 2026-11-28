package web

import (
	"fmt"
	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
	"strconv"
	"strings"
)

func parseNetworkInteger(get func(string) string, key string) int64 {
	raw := strings.TrimSpace(get(key))
	if raw == "" {
		return 0
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return -1
	}
	return v
}
func parseInterface(get func(string) string, id int64) domain.NetworkInterface {
	typ := get("owner_type")
	owner := parseNetworkInteger(get, "owner_id")
	if ref := get("owner"); ref != "" {
		var err error
		typ, owner, err = parseRef(ref)
		if err != nil {
			owner = -1
		}
	}
	return domain.NetworkInterface{ID: id, OwnerType: typ, OwnerID: owner, Name: strings.TrimSpace(get("name")), Kind: get("kind"), PortID: parseNetworkInteger(get, "port_id"), VLANID: parseNetworkInteger(get, "vlan_id"), MAC: strings.TrimSpace(get("mac")), MTU: int(parseNetworkInteger(get, "mtu")), Enabled: parseFormBool(get("enabled")), Notes: get("notes")}
}
func parseIPAddress(get func(string) string, id int64) domain.IPAddress {
	return domain.IPAddress{ID: id, Address: strings.TrimSpace(get("address")), InterfaceID: parseNetworkInteger(get, "interface_id"), Status: get("status"), DNSName: strings.TrimSpace(get("dns_name")), Notes: get("notes")}
}
func networkInterfaceLabel(v domain.NetworkInterface) string {
	return fmt.Sprintf("%s:%d / %s", v.OwnerType, v.OwnerID, v.Name)
}

func interfaceExtras(hosts *store.HostRepo, hardware *store.HardwareRepo, ports *store.PortRepo, vlans *store.VLANRepo) func() (map[string]any, error) {
	return func() (map[string]any, error) {
		owners, err := ownerOptions(hosts, hardware)
		if err != nil {
			return nil, err
		}
		ps, err := ports.List()
		if err != nil {
			return nil, err
		}
		vs, err := vlans.List()
		if err != nil {
			return nil, err
		}
		return map[string]any{"Owners": owners, "Ports": ps, "VLANs": vs, "Kinds": domain.InterfaceKinds}, nil
	}
}
func interfaceChildren(addresses *store.IPAddressRepo) func(domain.NetworkInterface, entityCatalog) (*childrenSection, error) {
	return func(v domain.NetworkInterface, cat entityCatalog) (*childrenSection, error) {
		items := []childRef{{Label: "Owner", Path: cat.path(v.OwnerType, v.OwnerID)}}
		if v.PortID != 0 {
			items = append(items, childRef{Label: "Physical port", Path: cat.path("port", v.PortID)})
		}
		if v.VLANID != 0 {
			items = append(items, childRef{Label: "VLAN", Path: cat.path("vlan", v.VLANID)})
		}
		ips, err := addresses.List()
		if err != nil {
			return nil, err
		}
		for _, ip := range ips {
			if ip.InterfaceID == v.ID {
				items = append(items, childRef{Label: ip.Address, Path: cat.path("ipaddress", ip.ID)})
			}
		}
		return &childrenSection{Title: "Assignments", Items: items}, nil
	}
}

func ownedNetworkChildren(ports *store.PortRepo, interfaces *store.InterfaceRepo, typ string, id int64, cat entityCatalog) (*childrenSection, error) {
	section, err := ownedPortsChildren(ports, typ, id, cat)
	if err != nil {
		return nil, err
	}
	if section == nil {
		section = &childrenSection{Title: "Connections"}
	}
	list, err := interfaces.List()
	if err != nil {
		return nil, err
	}
	for _, v := range list {
		if v.OwnerType == typ && v.OwnerID == id {
			section.Items = append(section.Items, childRef{Label: "Interface: " + v.Name, Path: cat.path("interface", v.ID)})
		}
	}
	return section, nil
}
