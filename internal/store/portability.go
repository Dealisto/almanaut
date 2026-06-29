package store

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/Dealisto/almanaut/internal/domain"
)

// Snapshot is the full inventory in a form that round-trips through YAML.
type Snapshot struct {
	Version       int                   `yaml:"version"`
	Hosts         []domain.Host         `yaml:"hosts"`
	Services      []domain.Service      `yaml:"services"`
	Networks      []domain.Network      `yaml:"networks"`
	Domains       []domain.Domain       `yaml:"domains"`
	Certificates  []domain.Certificate  `yaml:"certificates"`
	Backups       []domain.Backup       `yaml:"backups"`
	Hardware      []domain.Hardware     `yaml:"hardware"`
	Subscriptions []domain.Subscription `yaml:"subscriptions"`
	Accounts      []domain.Account      `yaml:"accounts"`
	Relationships []domain.Relationship `yaml:"relationships"`
	Tags          []domain.Tag          `yaml:"tags"`
}

// Export gathers the entire inventory into a Snapshot, reusing the existing
// per-repo List() read paths.
func Export(db *sql.DB) (Snapshot, error) {
	hosts, err := NewHostRepo(db).List()
	if err != nil {
		return Snapshot{}, err
	}
	services, err := NewServiceRepo(db).List()
	if err != nil {
		return Snapshot{}, err
	}
	networks, err := NewNetworkRepo(db).List()
	if err != nil {
		return Snapshot{}, err
	}
	domains, err := NewDomainRepo(db).List()
	if err != nil {
		return Snapshot{}, err
	}
	certificates, err := NewCertificateRepo(db).List()
	if err != nil {
		return Snapshot{}, err
	}
	backups, err := NewBackupRepo(db).List()
	if err != nil {
		return Snapshot{}, err
	}
	hardware, err := NewHardwareRepo(db).List()
	if err != nil {
		return Snapshot{}, err
	}
	subscriptions, err := NewSubscriptionRepo(db).List()
	if err != nil {
		return Snapshot{}, err
	}
	accounts, err := NewAccountRepo(db).List()
	if err != nil {
		return Snapshot{}, err
	}
	relationships, err := NewRelationshipRepo(db).List()
	if err != nil {
		return Snapshot{}, err
	}
	tags, err := NewTagRepo(db).List()
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{
		Version:       1,
		Hosts:         hosts,
		Services:      services,
		Networks:      networks,
		Domains:       domains,
		Certificates:  certificates,
		Backups:       backups,
		Hardware:      hardware,
		Subscriptions: subscriptions,
		Accounts:      accounts,
		Relationships: relationships,
		Tags:          tags,
	}, nil
}

// Import replaces the entire inventory with snap, in a single transaction.
// It validates every record first (aborting before any delete on the first
// error), then clears all tables and re-inserts each row with its original id
// so relationship and tag references stay valid. Any failure rolls back.
func Import(db *sql.DB, snap Snapshot) error {
	for _, h := range snap.Hosts {
		if err := h.Validate(); err != nil {
			return fmt.Errorf("host %d: %w", h.ID, err)
		}
	}
	for _, s := range snap.Services {
		if err := s.Validate(); err != nil {
			return fmt.Errorf("service %d: %w", s.ID, err)
		}
	}
	for _, n := range snap.Networks {
		if err := n.Validate(); err != nil {
			return fmt.Errorf("network %d: %w", n.ID, err)
		}
	}
	for _, d := range snap.Domains {
		if err := d.Validate(); err != nil {
			return fmt.Errorf("domain %d: %w", d.ID, err)
		}
	}
	for _, c := range snap.Certificates {
		if err := c.Validate(); err != nil {
			return fmt.Errorf("certificate %d: %w", c.ID, err)
		}
	}
	for _, b := range snap.Backups {
		if err := b.Validate(); err != nil {
			return fmt.Errorf("backup %d: %w", b.ID, err)
		}
	}
	for _, h := range snap.Hardware {
		if err := h.Validate(); err != nil {
			return fmt.Errorf("hardware %d: %w", h.ID, err)
		}
	}
	for _, s := range snap.Subscriptions {
		if err := s.Validate(); err != nil {
			return fmt.Errorf("subscription %d: %w", s.ID, err)
		}
	}
	for _, a := range snap.Accounts {
		if err := a.Validate(); err != nil {
			return fmt.Errorf("account %d: %w", a.ID, err)
		}
	}
	for _, rel := range snap.Relationships {
		if err := rel.Validate(); err != nil {
			return fmt.Errorf("relationship %d: %w", rel.ID, err)
		}
	}
	for _, tg := range snap.Tags {
		if err := tg.Validate(); err != nil {
			return fmt.Errorf("tag %d: %w", tg.ID, err)
		}
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback() // no-op once committed

	for _, table := range []string{"hosts", "services", "networks", "domains", "certificates", "backups", "hardware", "subscriptions", "accounts", "relationships", "tags"} {
		if _, err := tx.Exec("DELETE FROM " + table); err != nil {
			return fmt.Errorf("clear %s: %w", table, err)
		}
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
		if _, err := tx.Exec(
			`INSERT INTO hosts (id, name, type, os, cpu, ram, disk, status, ips, notes)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			h.ID, h.Name, h.Type, h.OS, h.CPU, h.RAM, h.Disk, h.Status, string(raw), h.Notes,
		); err != nil {
			return fmt.Errorf("insert host %d: %w", h.ID, err)
		}
	}
	for _, s := range snap.Services {
		if _, err := tx.Exec(
			`INSERT INTO services (id, name, kind, url, ports, category, notes)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			s.ID, s.Name, s.Kind, s.URL, s.Ports, s.Category, s.Notes,
		); err != nil {
			return fmt.Errorf("insert service %d: %w", s.ID, err)
		}
	}
	for _, n := range snap.Networks {
		if _, err := tx.Exec(
			`INSERT INTO networks (id, name, cidr, vlan, gateway, notes)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			n.ID, n.Name, n.CIDR, n.VLAN, n.Gateway, n.Notes,
		); err != nil {
			return fmt.Errorf("insert network %d: %w", n.ID, err)
		}
	}
	for _, d := range snap.Domains {
		if _, err := tx.Exec(
			`INSERT INTO domains (id, fqdn, provider, notes) VALUES (?, ?, ?, ?)`,
			d.ID, d.FQDN, d.Provider, d.Notes,
		); err != nil {
			return fmt.Errorf("insert domain %d: %w", d.ID, err)
		}
	}
	for _, c := range snap.Certificates {
		if _, err := tx.Exec(
			`INSERT INTO certificates (id, subject, issuer, expires_on, auto_renew, notes)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			c.ID, c.Subject, c.Issuer, c.ExpiresOn, boolToInt(c.AutoRenew), c.Notes,
		); err != nil {
			return fmt.Errorf("insert certificate %d: %w", c.ID, err)
		}
	}
	for _, b := range snap.Backups {
		if _, err := tx.Exec(
			`INSERT INTO backups (id, source, destination, frequency, last_run, notes)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			b.ID, b.Source, b.Destination, b.Frequency, b.LastRun, b.Notes,
		); err != nil {
			return fmt.Errorf("insert backup %d: %w", b.ID, err)
		}
	}
	for _, h := range snap.Hardware {
		if _, err := tx.Exec(
			`INSERT INTO hardware (id, name, kind, manufacturer, model, serial, location, purchase_date, warranty_end, status, notes)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			h.ID, h.Name, h.Kind, h.Manufacturer, h.Model, h.Serial, h.Location, h.PurchaseDate, h.WarrantyEnd, h.Status, h.Notes,
		); err != nil {
			return fmt.Errorf("insert hardware %d: %w", h.ID, err)
		}
	}
	for _, s := range snap.Subscriptions {
		if _, err := tx.Exec(
			`INSERT INTO subscriptions (id, name, kind, provider, amount, currency, billing_cycle, renewal_date, auto_renew, status, notes)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			s.ID, s.Name, s.Kind, s.Provider, s.Amount, s.Currency, s.BillingCycle, s.RenewalDate, boolToInt(s.AutoRenew), s.Status, s.Notes,
		); err != nil {
			return fmt.Errorf("insert subscription %d: %w", s.ID, err)
		}
	}
	for _, a := range snap.Accounts {
		if _, err := tx.Exec(
			`INSERT INTO accounts (id, name, kind, username, password_manager, secret_ref, url, status, notes)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			a.ID, a.Name, a.Kind, a.Username, a.PasswordManager, a.SecretRef, a.URL, a.Status, a.Notes,
		); err != nil {
			return fmt.Errorf("insert account %d: %w", a.ID, err)
		}
	}
	for _, rel := range snap.Relationships {
		if _, err := tx.Exec(
			`INSERT INTO relationships (id, from_type, from_id, to_type, to_id, kind)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			rel.ID, rel.FromType, rel.FromID, rel.ToType, rel.ToID, rel.Kind,
		); err != nil {
			return fmt.Errorf("insert relationship %d: %w", rel.ID, err)
		}
	}
	for _, tg := range snap.Tags {
		if _, err := tx.Exec(
			`INSERT INTO tags (id, entity_type, entity_id, name) VALUES (?, ?, ?, ?)`,
			tg.ID, tg.EntityType, tg.EntityID, tg.Name,
		); err != nil {
			return fmt.Errorf("insert tag %d: %w", tg.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
