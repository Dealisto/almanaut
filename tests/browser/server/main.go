// Command server provides an isolated loopback inventory for browser tests.
package main

import (
	"fmt"
	"log"
	"net/http/httptest"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/Dealisto/almanaut/internal/domain"
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
	}))
	defer srv.Close()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stop)
	fmt.Println(srv.URL)
	<-stop
	return nil
}
