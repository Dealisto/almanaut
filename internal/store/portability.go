package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
)

// Snapshot is the full inventory in a form that round-trips through YAML.
type Snapshot struct {
	Version        int                   `yaml:"version"`
	Hosts          []domain.Host         `yaml:"hosts"`
	Services       []domain.Service      `yaml:"services"`
	Networks       []domain.Network      `yaml:"networks"`
	Domains        []domain.Domain       `yaml:"domains"`
	Certificates   []domain.Certificate  `yaml:"certificates"`
	Backups        []domain.Backup       `yaml:"backups"`
	Hardware       []domain.Hardware     `yaml:"hardware"`
	Subscriptions  []domain.Subscription `yaml:"subscriptions"`
	Accounts       []domain.Account      `yaml:"accounts"`
	Sites          []domain.Site         `yaml:"sites"`
	Locations      []domain.Location     `yaml:"locations"`
	Racks          []domain.Rack         `yaml:"racks"`
	Relationships  []domain.Relationship `yaml:"relationships"`
	Tags           []domain.Tag          `yaml:"tags"`
	JournalEntries []domain.JournalEntry `yaml:"journal_entries"`
}

// Export gathers the entire inventory into a Snapshot, reusing the existing
// per-repo List() read paths. The first List error (in field order) is
// returned; later lists short-circuit once err is set.
//
// All fifteen lists run inside a single read transaction so the snapshot is
// internally consistent: without it a concurrent write between two lists could
// produce a YAML dump with, say, a relationship pointing at a host that the
// hosts list no longer contains — a corruption only discovered at re-import.
// WAL gives the transaction a stable view, and reading through the tx keeps
// every list on the one connection (no second-connection deadlock).
func Export(db *sql.DB) (Snapshot, error) {
	tx, err := db.Begin()
	if err != nil {
		return Snapshot{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback() // read-only: always roll back, nothing to commit

	var listErr error
	snap := Snapshot{
		Version:        1,
		Hosts:          exportList(&listErr, NewHostRepo(db).WithTx(tx).List),
		Services:       exportList(&listErr, NewServiceRepo(db).WithTx(tx).List),
		Networks:       exportList(&listErr, NewNetworkRepo(db).WithTx(tx).List),
		Domains:        exportList(&listErr, NewDomainRepo(db).WithTx(tx).List),
		Certificates:   exportList(&listErr, NewCertificateRepo(db).WithTx(tx).List),
		Backups:        exportList(&listErr, NewBackupRepo(db).WithTx(tx).List),
		Hardware:       exportList(&listErr, NewHardwareRepo(db).WithTx(tx).List),
		Subscriptions:  exportList(&listErr, NewSubscriptionRepo(db).WithTx(tx).List),
		Accounts:       exportList(&listErr, NewAccountRepo(db).WithTx(tx).List),
		Sites:          exportList(&listErr, NewSiteRepo(db).WithTx(tx).List),
		Locations:      exportList(&listErr, NewLocationRepo(db).WithTx(tx).List),
		Racks:          exportList(&listErr, NewRackRepo(db).WithTx(tx).List),
		Relationships:  exportList(&listErr, NewRelationshipRepo(db).WithTx(tx).List),
		Tags:           exportList(&listErr, NewTagRepo(db).WithTx(tx).List),
		JournalEntries: exportList(&listErr, NewJournalRepo(db).WithTx(tx).List),
	}
	if listErr != nil {
		return Snapshot{}, listErr
	}
	return snap, nil
}

// exportList calls list and records any error in *err, returning nil once a
// prior list has already failed so the caller keeps the first error.
func exportList[T any](err *error, list func() ([]T, error)) []T {
	if *err != nil {
		return nil
	}
	items, e := list()
	if e != nil {
		*err = e
	}
	return items
}

// Import replaces the entire inventory with snap, in a single transaction.
// It validates every record first (aborting before any delete on the first
// error), then clears all tables and re-inserts each row with its original id
// so relationship and tag references stay valid. Any failure rolls back.
func Import(db *sql.DB, snap Snapshot) error {
	for _, err := range []error{
		validateAll("host", snap.Hosts, func(h domain.Host) int64 { return h.ID }),
		validateAll("service", snap.Services, func(s domain.Service) int64 { return s.ID }),
		validateAll("network", snap.Networks, func(n domain.Network) int64 { return n.ID }),
		validateAll("domain", snap.Domains, func(d domain.Domain) int64 { return d.ID }),
		validateAll("certificate", snap.Certificates, func(c domain.Certificate) int64 { return c.ID }),
		validateAll("backup", snap.Backups, func(b domain.Backup) int64 { return b.ID }),
		validateAll("hardware", snap.Hardware, func(h domain.Hardware) int64 { return h.ID }),
		validateAll("subscription", snap.Subscriptions, func(s domain.Subscription) int64 { return s.ID }),
		validateAll("account", snap.Accounts, func(a domain.Account) int64 { return a.ID }),
		validateAll("site", snap.Sites, func(s domain.Site) int64 { return s.ID }),
		validateAll("location", snap.Locations, func(l domain.Location) int64 { return l.ID }),
		validateAll("rack", snap.Racks, func(k domain.Rack) int64 { return k.ID }),
		validateAll("relationship", snap.Relationships, func(r domain.Relationship) int64 { return r.ID }),
		validateAll("tag", snap.Tags, func(t domain.Tag) int64 { return t.ID }),
		validateAll("journal_entry", snap.JournalEntries, func(e domain.JournalEntry) int64 { return e.ID }),
	} {
		if err != nil {
			return err
		}
	}

	return WithTx(db, func(tx *sql.Tx) error {
		if err := replaceInventory(tx, snap); err != nil {
			return err
		}
		n := len(snap.Hosts) + len(snap.Services) + len(snap.Networks) + len(snap.Domains) +
			len(snap.Certificates) + len(snap.Backups) + len(snap.Hardware) +
			len(snap.Subscriptions) + len(snap.Accounts) +
			len(snap.Sites) + len(snap.Locations) + len(snap.Racks) +
			len(snap.Relationships) + len(snap.Tags) + len(snap.JournalEntries)
		return NewChangelogRepo(db).WithTx(tx).Create(ChangeEvent{
			EntityType: "", EntityID: 0, Label: "inventory", Action: domain.ActionImport,
			Changes:   []domain.FieldChange{{Field: "records", New: fmt.Sprintf("%d", n)}},
			CreatedAt: time.Now().UTC().Format(time.RFC3339),
		})
	})
}

// replaceInventory clears every table and re-inserts snap within tx. It must run
// inside WithTx, which owns begin/commit/rollback and is panic-safe, so any
// failure rolls the whole replacement back.
func replaceInventory(tx *sql.Tx, snap Snapshot) error {
	for _, table := range []string{"hosts", "services", "networks", "domains", "certificates", "backups", "hardware", "subscriptions", "accounts", "sites", "locations", "racks", "relationships", "tags", "journal_entries"} {
		if _, err := tx.Exec("DELETE FROM " + table); err != nil {
			return fmt.Errorf("clear %s: %w", table, err)
		}
	}

	// insert runs one INSERT and tags any failure with the entity label and id.
	insert := func(label string, id int64, query string, args ...any) error {
		if _, err := tx.Exec(query, args...); err != nil {
			return fmt.Errorf("insert %s %d: %w", label, id, err)
		}
		return nil
	}

	for _, h := range snap.Hosts {
		ips := h.IPs
		if ips == nil {
			ips = []string{}
		}
		raw, err := json.Marshal(ips)
		if err != nil {
			return fmt.Errorf("marshal ips for host %d: %w", h.ID, err)
		}
		if err := insert("host", h.ID,
			`INSERT INTO hosts (id, name, type, os, cpu, ram, disk, status, ips, notes)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			h.ID, h.Name, h.Type, h.OS, h.CPU, h.RAM, h.Disk, h.Status, string(raw), h.Notes); err != nil {
			return err
		}
	}
	for _, s := range snap.Services {
		if err := insert("service", s.ID,
			`INSERT INTO services (id, name, kind, url, ports, category, notes)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			s.ID, s.Name, s.Kind, s.URL, s.Ports, s.Category, s.Notes); err != nil {
			return err
		}
	}
	for _, n := range snap.Networks {
		if err := insert("network", n.ID,
			`INSERT INTO networks (id, name, cidr, vlan, gateway, notes)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			n.ID, n.Name, n.CIDR, n.VLAN, n.Gateway, n.Notes); err != nil {
			return err
		}
	}
	for _, d := range snap.Domains {
		if err := insert("domain", d.ID,
			`INSERT INTO domains (id, fqdn, provider, notes) VALUES (?, ?, ?, ?)`,
			d.ID, d.FQDN, d.Provider, d.Notes); err != nil {
			return err
		}
	}
	for _, c := range snap.Certificates {
		if err := insert("certificate", c.ID,
			`INSERT INTO certificates (id, subject, issuer, expires_on, auto_renew, notes)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			c.ID, c.Subject, c.Issuer, c.ExpiresOn, boolToInt(c.AutoRenew), c.Notes); err != nil {
			return err
		}
	}
	for _, b := range snap.Backups {
		if err := insert("backup", b.ID,
			`INSERT INTO backups (id, source, destination, frequency, last_run, notes)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			b.ID, b.Source, b.Destination, b.Frequency, b.LastRun, b.Notes); err != nil {
			return err
		}
	}
	for _, h := range snap.Hardware {
		if err := insert("hardware", h.ID,
			`INSERT INTO hardware (id, name, kind, manufacturer, model, serial, location, purchase_date, warranty_end, status, notes)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			h.ID, h.Name, h.Kind, h.Manufacturer, h.Model, h.Serial, h.Location, h.PurchaseDate, h.WarrantyEnd, h.Status, h.Notes); err != nil {
			return err
		}
	}
	for _, s := range snap.Subscriptions {
		if err := insert("subscription", s.ID,
			`INSERT INTO subscriptions (id, name, kind, provider, amount, currency, billing_cycle, renewal_date, auto_renew, status, notes)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			s.ID, s.Name, s.Kind, s.Provider, s.Amount, s.Currency, s.BillingCycle, s.RenewalDate, boolToInt(s.AutoRenew), s.Status, s.Notes); err != nil {
			return err
		}
	}
	for _, a := range snap.Accounts {
		if err := insert("account", a.ID,
			`INSERT INTO accounts (id, name, kind, username, password_manager, secret_ref, url, status, notes)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			a.ID, a.Name, a.Kind, a.Username, a.PasswordManager, a.SecretRef, a.URL, a.Status, a.Notes); err != nil {
			return err
		}
	}
	for _, s := range snap.Sites {
		if err := insert("site", s.ID,
			`INSERT INTO sites (id, name, address, notes) VALUES (?, ?, ?, ?)`,
			s.ID, s.Name, s.Address, s.Notes); err != nil {
			return err
		}
	}
	for _, l := range snap.Locations {
		if err := insert("location", l.ID,
			`INSERT INTO locations (id, name, site_id, notes) VALUES (?, ?, ?, ?)`,
			l.ID, l.Name, l.SiteID, l.Notes); err != nil {
			return err
		}
	}
	for _, k := range snap.Racks {
		if err := insert("rack", k.ID,
			`INSERT INTO racks (id, name, location_id, u_height, notes) VALUES (?, ?, ?, ?, ?)`,
			k.ID, k.Name, k.LocationID, k.UHeight, k.Notes); err != nil {
			return err
		}
	}
	for _, rel := range snap.Relationships {
		if err := insert("relationship", rel.ID,
			`INSERT INTO relationships (id, from_type, from_id, to_type, to_id, kind)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			rel.ID, rel.FromType, rel.FromID, rel.ToType, rel.ToID, rel.Kind); err != nil {
			return err
		}
	}
	for _, tg := range snap.Tags {
		if err := insert("tag", tg.ID,
			`INSERT INTO tags (id, entity_type, entity_id, name) VALUES (?, ?, ?, ?)`,
			tg.ID, tg.EntityType, tg.EntityID, tg.Name); err != nil {
			return err
		}
	}
	for _, e := range snap.JournalEntries {
		if err := insert("journal_entry", e.ID,
			`INSERT INTO journal_entries (id, entity_type, entity_id, kind, body, created_at)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			e.ID, e.EntityType, e.EntityID, e.Kind, e.Body, e.CreatedAt); err != nil {
			return err
		}
	}

	return nil
}

// validateAll validates every item, returning the first failure tagged with the
// entity label and id. id extracts the record id for the error message.
func validateAll[T interface{ Validate() error }](label string, items []T, id func(T) int64) error {
	for _, it := range items {
		if err := it.Validate(); err != nil {
			return fmt.Errorf("%s %d: %w", label, id(it), err)
		}
	}
	return nil
}
