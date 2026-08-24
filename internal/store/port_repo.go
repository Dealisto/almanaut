package store

import (
	"database/sql"
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

func (r *PortRepo) Create(v domain.Port) (int64, error) {
	res, err := r.db.Exec(
		`INSERT INTO ports (owner_type, owner_id, nic_id, name, mac, mgmt_only, peer_port_id, notes) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		v.OwnerType, v.OwnerID, v.NICID, v.Name, v.MAC, v.MgmtOnly, v.PeerPortID, v.Notes,
	)
	if err != nil {
		return 0, fmt.Errorf("insert port: %w", err)
	}
	return res.LastInsertId()
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

func (r *PortRepo) Update(v domain.Port) error {
	res, err := r.db.Exec(
		`UPDATE ports SET owner_type=?, owner_id=?, nic_id=?, name=?, mac=?, mgmt_only=?, peer_port_id=?, notes=? WHERE id=?`,
		v.OwnerType, v.OwnerID, v.NICID, v.Name, v.MAC, v.MgmtOnly, v.PeerPortID, v.Notes, v.ID,
	)
	if err != nil {
		return fmt.Errorf("update port: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}

// Delete removes the port and clears any peer pointer on other ports that
// referenced it, so no dangling links survive.
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
// names, and the effective peer. The peer link is stored on one side only, so
// each port first uses its own pointer and otherwise the first port pointing
// back at it; the label is "<peer owner> / <peer name>".
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

	// The peer index needs every port, not just the slice being decorated.
	all, err := r.listRaw()
	if err != nil {
		return nil, err
	}
	byID := map[int64]domain.Port{}
	reverse := map[int64]int64{} // pointed-at port id → first port pointing at it
	for _, p := range all {
		byID[p.ID] = p
		if p.PeerPortID != 0 {
			if _, ok := reverse[p.PeerPortID]; !ok {
				reverse[p.PeerPortID] = p.ID
			}
		}
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
		peerID := p.PeerPortID
		if peerID == 0 {
			peerID = reverse[p.ID]
		}
		if peerID != 0 {
			p.PeerID = peerID
			if peer, ok := byID[peerID]; ok {
				p.PeerLabel = ownerName(peer.OwnerType, peer.OwnerID) + " / " + peer.Name
			} else {
				p.PeerLabel = fmt.Sprintf("port:%d (deleted)", peerID)
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
