// Command server provides an isolated loopback inventory for browser tests.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http/httptest"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/Dealisto/almanaut/internal/discovery"
	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/snmp"
	"github.com/Dealisto/almanaut/internal/store"
	"github.com/Dealisto/almanaut/internal/web"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	if len(os.Args) != 2 {
		return fmt.Errorf("usage: browser-test-server TEMP_DIRECTORY")
	}
	db, err := store.Open(filepath.Join(os.Args[1], "browser.db"))
	if err != nil {
		return err
	}
	defer db.Close()
	if err := store.Migrate(db, ""); err != nil {
		return err
	}
	// Exercise attribute escaping in every entity whose name permits free text.
	name := `Browser "quoted" & <script>window.injected = true</script>`
	if err := store.Import(db, store.Snapshot{
		Version:       1,
		Hosts:         []domain.Host{{ID: 1, Name: name, Type: "physical", RackID: 1, RackPosition: 1, UHeight: 2}},
		Services:      []domain.Service{{ID: 1, Name: name, Kind: "native"}},
		Networks:      []domain.Network{{ID: 1, Name: name, CIDR: "192.0.2.0/24"}},
		Domains:       []domain.Domain{{ID: 1, FQDN: "browser.example.test"}},
		Certificates:  []domain.Certificate{{ID: 1, Subject: name, ExpiresOn: "2099-01-01"}},
		Backups:       []domain.Backup{{ID: 1, Source: name}},
		Hardware:      []domain.Hardware{{ID: 1, Name: name, RackID: 1, RackPosition: 1, UHeight: 2}},
		Subscriptions: []domain.Subscription{{ID: 1, Name: name}},
		Accounts:      []domain.Account{{ID: 1, Name: name}},
		Sites:         []domain.Site{{ID: 1, Name: name}},
		Locations:     []domain.Location{{ID: 1, Name: name, SiteID: 1}},
		Racks:         []domain.Rack{{ID: 1, Name: name, LocationID: 1, UHeight: 42}},
		Contacts:      []domain.Contact{{ID: 1, Name: name}},
		VLANs:         []domain.VLAN{{ID: 1, Name: name, VID: 42}},
		Reservations:  []domain.Reservation{{ID: 1, Name: name, NetworkID: 1, StartIP: "192.0.2.10", EndIP: "192.0.2.10"}},
		NICs:          []domain.NIC{{ID: 1, Name: name, HostID: 1, Kind: "onboard"}},
		Ports:         []domain.Port{{ID: 1, Name: name, OwnerType: "host", OwnerID: 1, NICID: 1}},
	}); err != nil {
		return err
	}
	// Populate a cached observation without opening a network socket. Long
	// unbroken and wide strings exercise mobile wrapping and HTML escaping.
	snmpManager := snmp.NewManager(db, "browser", browserSNMPScanner{}, "browser-fixture")
	if _, err := snmpManager.Scan(context.Background()); err != nil {
		return err
	}
	// Authentication is disabled only in this test helper; httptest binds a
	// random loopback port and the database belongs to the runner's temp dir.
	srv := httptest.NewServer(web.New(web.Config{
		DB: db, Hosts: store.NewHostRepo(db), Services: store.NewServiceRepo(db),
		Networks: store.NewNetworkRepo(db), Domains: store.NewDomainRepo(db),
		Certificates: store.NewCertificateRepo(db), Backups: store.NewBackupRepo(db),
		Hardware: store.NewHardwareRepo(db), Subscriptions: store.NewSubscriptionRepo(db),
		Accounts: store.NewAccountRepo(db), Sites: store.NewSiteRepo(db),
		Locations: store.NewLocationRepo(db), Racks: store.NewRackRepo(db),
		Contacts: store.NewContactRepo(db), Relationships: store.NewRelationshipRepo(db),
		Tags: store.NewTagRepo(db), VLANs: store.NewVLANRepo(db),
		Reservations: store.NewReservationRepo(db),
		Docker:       browserScanner{},
		SNMPSources:  []web.SNMPSource{{ID: "browser", Name: "Browser switch observations", OwnerType: "hardware", OwnerID: 1, Scanner: snmpManager, Fingerprint: "browser-fixture"}},
	}))
	defer srv.Close()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stop)
	fmt.Println(srv.URL)
	<-stop
	return nil
}

// Browser discovery fixtures never connect to a real container engine.
type browserScanner struct{}

func (browserScanner) Containers(context.Context) ([]discovery.Container, error) {
	return []discovery.Container{
		{ID: "browser-first", Name: "Browser first container"},
		{ID: "browser-second", Name: "Browser second container"},
	}, nil
}

// browserSNMPScanner is a static test fixture, never a device transport.
type browserSNMPScanner struct{}

func (browserSNMPScanner) Scan(context.Context) (snmp.Observation, error) {
	long := strings.Repeat("UnbrokenObservedIdentifier", 14)
	return snmp.Observation{
		SystemName:  "Browser <script>window.injected = true</script> " + long,
		Description: strings.Repeat("Switch界🌐", 32), ObjectID: ".1.3.6.1.4.1.99999.1",
		Manufacturer: "Fixture manufacturer", Model: "Browser switch", Serial: long,
		LLDPComplete: true,
		Interfaces: []snmp.Interface{
			{Index: 7, Name: "Ethernet7", Description: "Observed uplink <script>window.injected = true</script>", Alias: long, Type: 6, MAC: "02:00:00:00:00:07", SpeedMbps: 10000, AdminStatus: 1, OperStatus: 1},
			{Index: 8, Name: strings.Repeat("Wide界🌐", 16), Description: "Observed unused port", Type: 6, MAC: "02:00:00:00:00:08", SpeedMbps: 1000, AdminStatus: 2, OperStatus: 2},
		},
		Neighbors: []snmp.Neighbor{{LocalNumber: 19, LocalSubtype: 5, LocalID: "Ethernet7", LocalDescription: long, ChassisSubtype: 4, ChassisID: "02:00:00:00:00:99", PortSubtype: 5, PortID: long, PortDescription: "Remote port <script>window.injected = true</script>", SystemName: "Unmapped observed neighbor"}},
	}, nil
}
