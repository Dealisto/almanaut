package store

import (
	"errors"
	"github.com/Dealisto/almanaut/internal/domain"
	"reflect"
	"testing"
)

func TestStructuredNetworkInventory(t *testing.T) {
	db := newTestDB(t)
	hosts := NewHostRepo(db)
	h, err := hosts.Create(domain.Host{Name: "host", Type: "physical", IPs: []string{"192.0.2.1"}})
	if err != nil {
		t.Fatal(err)
	}
	interfaces := NewInterfaceRepo(db)
	addresses := NewIPAddressRepo(db)
	iface := domain.NetworkInterface{OwnerType: "host", OwnerID: h, Name: "eth0", Kind: "physical", Enabled: true, MTU: 1500}
	iface.ID, err = interfaces.Create(iface)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = interfaces.Create(iface); err == nil {
		t.Fatal("duplicate interface accepted")
	}
	ip := domain.IPAddress{Address: "192.0.2.1/24", InterfaceID: iface.ID, Status: "active"}
	ip.ID, err = addresses.Create(ip)
	if err != nil {
		t.Fatal(err)
	}
	if err = interfaces.Delete(iface.ID); err == nil {
		t.Fatal("deleted assigned interface")
	}
	if _, err = addresses.Create(domain.IPAddress{Address: "192.0.2.8", Status: "reserved", InterfaceID: 999}); err == nil {
		t.Fatal("accepted missing interface")
	}
	id, err := addresses.Create(domain.IPAddress{Address: "2001:0DB8::1/64", Status: "reserved"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := addresses.Get(id)
	if err != nil || got.Address != "2001:db8::1/64" {
		t.Fatalf("canonical address: %+v %v", got, err)
	}
	projected, err := IPAMHosts(db)
	if err != nil {
		t.Fatal(err)
	}
	report := domain.BuildIPAM([]domain.Network{{ID: 1, CIDR: "192.0.2.0/29"}}, projected, nil)
	if report.Networks[0].UsedCount != 1 || len(report.Networks[0].Conflicts) != 0 || report.Networks[0].NextFree != "192.0.2.2" {
		t.Fatalf("legacy/structured union: %+v", report.Networks[0])
	}
	if len(report.Unassigned) != 1 || report.Unassigned[0].Ref().Type != "ipaddress" {
		t.Fatalf("unassigned explicit address lost: %+v", report.Unassigned)
	}
	snap, err := Export(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Interfaces) != 1 || len(snap.IPAddresses) != 2 {
		t.Fatalf("missing export: %+v", snap)
	}
	if err = Import(db, snap); err != nil {
		t.Fatal(err)
	}
	restored, err := Export(db)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(snap.Interfaces, restored.Interfaces) || !reflect.DeepEqual(snap.IPAddresses, restored.IPAddresses) {
		t.Fatal("structured fields changed on restore")
	}
	if err = addresses.Delete(ip.ID); err != nil {
		t.Fatal(err)
	}
	if err = interfaces.Delete(iface.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = interfaces.Get(iface.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestStructuredAllocationBlocksNextFreeAcrossOverlaps(t *testing.T) {
	db := newTestDB(t)
	addresses := NewIPAddressRepo(db)
	for _, ip := range []string{"192.0.2.1/32", "192.0.2.2/32"} {
		if _, err := addresses.Create(domain.IPAddress{Address: ip, Status: "deprecated"}); err != nil {
			t.Fatal(err)
		}
	}
	hosts, err := IPAMHosts(db)
	if err != nil {
		t.Fatal(err)
	}
	nets := []domain.Network{{ID: 1, CIDR: "192.0.2.0/29"}, {ID: 2, CIDR: "192.0.2.0/31"}}
	report := domain.BuildIPAM(nets, hosts, nil)
	for _, n := range nets {
		single, ok := domain.BuildNetworkUsage(n.ID, nets, hosts, nil)
		if !ok {
			t.Fatal("missing network")
		}
		for _, full := range report.Networks {
			if full.Network.ID == n.ID && !reflect.DeepEqual(full, single) {
				t.Fatal("attribution differs")
			}
		}
	}
	if report.Networks[0].NextFree != "192.0.2.3" {
		t.Fatal(report.Networks[0].NextFree)
	}
}

func TestStructuredOwnerParityAndPortOwnership(t *testing.T) {
	db := newTestDB(t)
	hostID, _ := NewHostRepo(db).Create(domain.Host{Name: "host", Type: "physical"})
	hardwareID, _ := NewHardwareRepo(db).Create(domain.Hardware{Name: "switch", Kind: "switch"})
	interfaces := NewInterfaceRepo(db)
	addresses := NewIPAddressRepo(db)
	for _, owner := range []domain.EntityRef{{Type: "host", ID: hostID}, {Type: "hardware", ID: hardwareID}} {
		for _, name := range []string{"first", "second"} {
			id, err := interfaces.Create(domain.NetworkInterface{OwnerType: owner.Type, OwnerID: owner.ID, Name: name, Kind: "virtual"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = addresses.Create(domain.IPAddress{Address: "192.0.2.7", Status: "active", InterfaceID: id}); err != nil {
				t.Fatal(err)
			}
		}
	}
	projected, err := IPAMHosts(db)
	if err != nil {
		t.Fatal(err)
	}
	conflicts := domain.BuildIPAMConflicts(nil, projected).DuplicateIPs
	if len(conflicts) != 1 || len(conflicts[0].Hosts) != 2 {
		t.Fatalf("expected one claim per actual owner: %+v", conflicts)
	}
	port := domain.Port{OwnerType: "host", OwnerID: hostID, Name: "physical"}
	port.ID, err = NewPortRepo(db).Create(port)
	if err != nil {
		t.Fatal(err)
	}
	v := domain.NetworkInterface{OwnerType: "host", OwnerID: hostID, Name: "wired", Kind: "physical", PortID: port.ID}
	v.ID, err = interfaces.Create(v)
	if err != nil {
		t.Fatal(err)
	}
	port.OwnerType = "hardware"
	port.OwnerID = hardwareID
	if err = NewPortRepo(db).Update(port); err == nil {
		t.Fatal("moved port away from assigned interface")
	}
	snap, err := Export(db)
	if err != nil {
		t.Fatal(err)
	}
	snap.Ports[0] = port
	if err = Import(db, snap); err == nil {
		t.Fatal("imported inconsistent ownership")
	}
	var validation *ImportValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("expected validation error: %v", err)
	}
	if stored, err := NewPortRepo(db).Get(port.ID); err != nil || stored.OwnerType != "host" {
		t.Fatalf("invalid import changed inventory: %+v %v", stored, err)
	}
}
