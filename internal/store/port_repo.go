package store

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/Dealisto/almanaut/internal/domain"
)

// PortRepo persists Port entities in SQLite.
type PortRepo struct{ db DBTX }

func NewPortRepo(db *sql.DB) *PortRepo          { return &PortRepo{db: db} }
func (r *PortRepo) WithTx(tx *sql.Tx) *PortRepo { return &PortRepo{db: tx} }
func (r *PortRepo) DeleteTx(tx *sql.Tx, id int64) error {
	return r.WithTx(tx).Delete(id)
}
func (r *PortRepo) CreateTx(tx *sql.Tx, v domain.Port) (int64, error) {
	return r.WithTx(tx).Create(v)
}
func (r *PortRepo) UpdateTx(tx *sql.Tx, v domain.Port) error {
	return r.WithTx(tx).Update(v)
}
func (r *PortRepo) GetTx(tx *sql.Tx, id int64) (domain.Port, error) {
	return r.WithTx(tx).Get(id)
}

func (r *PortRepo) Count() (int, error) {
	var n int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM ports`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count ports: %w", err)
	}
	return n, nil
}

const portColumns = `id, owner_type, owner_id, nic_id, name, mac, mgmt_only, peer_port_id, notes`

// Create inserts v and, when v.PeerPortID is set, connects it to that port.
// The connection reads and writes other rows, so callers outside tests use the
// tx-bound CreateTx.
func (r *PortRepo) Create(v domain.Port) (int64, error) {
	res, err := r.db.Exec(
		`INSERT INTO ports (owner_type, owner_id, nic_id, name, mac, mgmt_only, peer_port_id, notes) VALUES (?, ?, ?, ?, ?, ?, 0, ?)`,
		v.OwnerType, v.OwnerID, v.NICID, v.Name, v.MAC, v.MgmtOnly, v.Notes,
	)
	if err != nil {
		return 0, fmt.Errorf("insert port: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("port id: %w", err)
	}
	if err := r.connect(id, v.PeerPortID); err != nil {
		return 0, err
	}
	return id, nil
}

func (r *PortRepo) Get(id int64) (domain.Port, error) {
	row := r.db.QueryRow(`SELECT `+portColumns+` FROM ports WHERE id = ?`, id)
	v, err := scanPort(row)
	if err != nil {
		return domain.Port{}, err
	}
	decorated, err := r.decorate([]domain.Port{v})
	if err != nil {
		return domain.Port{}, err
	}
	return decorated[0], nil
}

// List returns all ports grouped by owner, with names in natural order
// (length before lexicographic, so "Port 2" sorts before "Port 10").
func (r *PortRepo) List() ([]domain.Port, error) {
	items, err := r.listRaw()
	if err != nil {
		return nil, err
	}
	return r.decorate(items)
}

func (r *PortRepo) listRaw() ([]domain.Port, error) {
	rows, err := r.db.Query(`SELECT ` + portColumns + ` FROM ports ORDER BY owner_type, owner_id, LENGTH(name), name`)
	if err != nil {
		return nil, fmt.Errorf("query ports: %w", err)
	}
	defer rows.Close()
	items := []domain.Port{}
	for rows.Next() {
		v, err := scanPort(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}

// Update overwrites the port and connects it to v.PeerPortID (0 disconnects
// it). Like Create, it touches other rows; callers use the tx-bound UpdateTx.
func (r *PortRepo) Update(v domain.Port) error {
	res, err := r.db.Exec(
		`UPDATE ports SET owner_type=?, owner_id=?, nic_id=?, name=?, mac=?, mgmt_only=?, notes=? WHERE id=?`,
		v.OwnerType, v.OwnerID, v.NICID, v.Name, v.MAC, v.MgmtOnly, v.Notes, v.ID,
	)
	if err != nil {
		return fmt.Errorf("update port: %w", err)
	}
	if err := rowsAffectedOrNotFound(res); err != nil {
		return err
	}
	return r.connect(v.ID, v.PeerPortID)
}

// connect makes ports id and peer name each other in peer_port_id, after
// disconnecting id from its current peer; peer 0 only disconnects. Storing the
// link on both sides keeps peer_port_id true for every port, so an API client
// that reads a port and writes it back unchanged keeps its connection. A peer
// already connected to a third port is refused rather than silently re-cabled.
func (r *PortRepo) connect(id, peer int64) error {
	if peer == id {
		return &ConstraintError{Reason: "a port cannot be connected to itself"}
	}
	if peer != 0 {
		var name string
		var current int64
		err := r.db.QueryRow(`SELECT name, peer_port_id FROM ports WHERE id = ?`, peer).Scan(&name, &current)
		if errors.Is(err, sql.ErrNoRows) {
			return &ConstraintError{Reason: fmt.Sprintf("connected port %d does not exist", peer)}
		}
		if err != nil {
			return fmt.Errorf("read peer port: %w", err)
		}
		if current != 0 && current != id {
			return &ConstraintError{Reason: fmt.Sprintf("port %q is already connected to another port; disconnect it first", name)}
		}
	}
	// Disconnect id from its old peer (on both sides) before cabling the new one,
	// so the unique index on peer_port_id never sees two ports naming one peer.
	if _, err := r.db.Exec(`UPDATE ports SET peer_port_id = 0 WHERE id = ? OR peer_port_id = ?`, id, id); err != nil {
		return fmt.Errorf("disconnect port: %w", err)
	}
	if peer == 0 {
		return nil
	}
	if _, err := r.db.Exec(
		`UPDATE ports SET peer_port_id = CASE id WHEN ? THEN ? ELSE ? END WHERE id IN (?, ?)`,
		id, peer, id, id, peer,
	); err != nil {
		return fmt.Errorf("connect port: %w", err)
	}
	return nil
}

// Delete removes the port and clears its peer's pointer back to it, so no
// dangling link survives.
func (r *PortRepo) Delete(id int64) error {
	if _, err := r.db.Exec(`UPDATE ports SET peer_port_id = 0 WHERE peer_port_id = ?`, id); err != nil {
		return fmt.Errorf("clear peer refs: %w", err)
	}
	if _, err := r.db.Exec(`DELETE FROM ports WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete port: %w", err)
	}
	return nil
}

// decorate fills the derived fields on each port: the owner and NIC display
// names, and the peer label "<peer owner> / <peer name>".
func (r *PortRepo) decorate(items []domain.Port) ([]domain.Port, error) {
	if len(items) == 0 {
		return items, nil
	}
	hosts, err := entityNames(r.db, "hosts")
	if err != nil {
		return nil, err
	}
	hardware, err := entityNames(r.db, "hardware")
	if err != nil {
		return nil, err
	}
	nics, err := entityNames(r.db, "nics")
	if err != nil {
		return nil, err
	}
	ownerName := func(ownerType string, ownerID int64) string {
		names := hosts
		if ownerType == "hardware" {
			names = hardware
		}
		if name, ok := names[ownerID]; ok {
			return name
		}
		return fmt.Sprintf("%s:%d (deleted)", ownerType, ownerID)
	}

	// A peer can belong to any owner, so the label needs every port, not just
	// the slice being decorated.
	all, err := r.listRaw()
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]domain.Port, len(all))
	for _, p := range all {
		byID[p.ID] = p
	}

	for i := range items {
		p := &items[i]
		p.OwnerName = ownerName(p.OwnerType, p.OwnerID)
		if p.NICID != 0 {
			name, ok := nics[p.NICID]
			if !ok {
				name = fmt.Sprintf("nic:%d (deleted)", p.NICID)
			}
			p.NICName = name
		}
		if p.PeerPortID != 0 {
			if peer, ok := byID[p.PeerPortID]; ok {
				p.PeerLabel = ownerName(peer.OwnerType, peer.OwnerID) + " / " + peer.Name
			} else {
				p.PeerLabel = fmt.Sprintf("port:%d (deleted)", p.PeerPortID)
			}
		}
	}
	return items, nil
}

func scanPort(s scanner) (domain.Port, error) {
	var v domain.Port
	if err := s.Scan(&v.ID, &v.OwnerType, &v.OwnerID, &v.NICID, &v.Name, &v.MAC, &v.MgmtOnly, &v.PeerPortID, &v.Notes); err != nil {
		return domain.Port{}, notFound(fmt.Errorf("scan port: %w", err))
	}
	return v, nil
}
