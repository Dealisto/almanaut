package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/Dealisto/almanaut/internal/domain"
)

const (
	TopologySeedLimit  = 60
	TopologyNodeLimit  = 100
	TopologyCableLimit = 200
)

const topologyOwners = `WITH owners AS (
 SELECT 'host' AS kind, id, name, rack_id FROM hosts
 UNION ALL SELECT 'hardware', id, name, rack_id FROM hardware
)
`

const topologyNodeSelect = `SELECT o.kind, o.id, substr(o.name,1,160),
 COALESCE(substr(r.name,1,160),''), COALESCE(substr(s.name,1,160),'')
 FROM owners o LEFT JOIN racks r ON r.id=o.rack_id
 LEFT JOIN locations l ON l.id=r.location_id LEFT JOIN sites s ON s.id=l.site_id `

// ReadTopology loads at most 60 scoped devices, 100 nodes and 200 cables. All
// queries share one read-only snapshot, without loading full inventory records.
func ReadTopology(ctx context.Context, db *sql.DB, scope domain.TopologyScope) (domain.Topology, error) {
	out := domain.Topology{}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	for _, spec := range []struct {
		table    string
		selected int64
		target   *[]domain.TopologyChoice
	}{{"sites", scope.SiteID, &out.Sites}, {"racks", scope.RackID, &out.Racks}} {
		choices, err := tx.QueryContext(ctx, "SELECT id,substr(name,1,160) FROM "+spec.table+" ORDER BY (id=?) DESC,name,id LIMIT 101", spec.selected)
		if err != nil {
			return out, err
		}
		for choices.Next() {
			var choice domain.TopologyChoice
			if err := choices.Scan(&choice.ID, &choice.Name); err != nil {
				choices.Close()
				return out, err
			}
			if len(*spec.target) == 100 {
				out.ChoicesTruncated = true
				break
			}
			*spec.target = append(*spec.target, choice)
		}
		err = choices.Err()
		choices.Close()
		if err != nil {
			return out, err
		}
		if spec.selected > 0 && (len(*spec.target) == 0 || (*spec.target)[0].ID != spec.selected) {
			return out, ErrNotFound
		}
	}
	where, args := "1=1", []any{}
	switch {
	case scope.Device.ID > 0:
		where, args = "o.kind=? AND o.id=?", []any{scope.Device.Type, scope.Device.ID}
	case scope.RackID > 0:
		where, args = "r.id=?", []any{scope.RackID}
	case scope.SiteID > 0:
		where, args = "s.id=?", []any{scope.SiteID}
	case scope.Unassigned:
		where = "s.id IS NULL"
	}
	args = append(args, TopologySeedLimit+1)
	rows, err := tx.QueryContext(ctx, topologyOwners+topologyNodeSelect+" WHERE "+where+" ORDER BY o.kind,o.id LIMIT ?", args...)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var node domain.TopologyNode
		if err := rows.Scan(&node.Ref.Type, &node.Ref.ID, &node.Name, &node.RackName, &node.SiteName); err != nil {
			rows.Close()
			return out, err
		}
		if len(out.Nodes) == TopologySeedLimit {
			out.Truncated = true
			break
		}
		out.Nodes = append(out.Nodes, node)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	if scope.Device.ID > 0 && len(out.Nodes) == 0 {
		return out, ErrNotFound
	}
	if len(out.Nodes) == 0 {
		return out, tx.Commit()
	}
	seeds := make(map[domain.EntityRef]bool, len(out.Nodes))
	nodes := make(map[domain.EntityRef]bool, len(out.Nodes))
	var filters []string
	args = nil
	for _, node := range out.Nodes {
		seeds[node.Ref] = true
		nodes[node.Ref] = true
		filters = append(filters, "(p.owner_type=? AND p.owner_id=?)")
		args = append(args, node.Ref.Type, node.Ref.ID)
	}
	// Reciprocal links can appear twice, so allow twice the cable budget before
	// deduplication. A nonreciprocal or missing peer is reported, never invented.
	args = append(args, 2*TopologyCableLimit+1)
	rows, err = tx.QueryContext(ctx, `SELECT p.id,p.owner_type,p.owner_id,substr(p.name,1,160),
 COALESCE(q.id,0),COALESCE(q.owner_type,''),COALESCE(q.owner_id,0),COALESCE(substr(q.name,1,160),''),COALESCE(q.peer_port_id,0)
 FROM ports p LEFT JOIN ports q ON q.id=p.peer_port_id
 WHERE p.peer_port_id != 0 AND (`+strings.Join(filters, " OR ")+`) ORDER BY p.id LIMIT ?`, args...)
	if err != nil {
		return out, err
	}
	seen := make(map[[2]int64]bool)
	var peers []domain.EntityRef
	count := 0
	for rows.Next() {
		count++
		if count > 2*TopologyCableLimit {
			out.Truncated = true
			break
		}
		var cable domain.TopologyCable
		var back int64
		if err := rows.Scan(&cable.FromPort, &cable.From.Type, &cable.From.ID, &cable.FromName, &cable.ToPort, &cable.To.Type, &cable.To.ID, &cable.ToName, &back); err != nil {
			rows.Close()
			return out, err
		}
		if cable.ToPort == 0 || back != cable.FromPort || cable.FromPort == cable.ToPort || (cable.To.Type != "host" && cable.To.Type != "hardware") {
			out.Broken++
			continue
		}
		key := [2]int64{min(cable.FromPort, cable.ToPort), max(cable.FromPort, cable.ToPort)}
		if seen[key] {
			continue
		}
		seen[key] = true
		if len(out.Cables) == TopologyCableLimit {
			out.Truncated = true
			continue
		}
		if !nodes[cable.To] {
			if len(nodes) == TopologyNodeLimit {
				out.Truncated = true
				continue
			}
			nodes[cable.To] = true
			peers = append(peers, cable.To)
		}
		out.Cables = append(out.Cables, cable)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	if len(peers) > 0 {
		filters = nil
		args = nil
		for _, ref := range peers {
			filters = append(filters, "(o.kind=? AND o.id=?)")
			args = append(args, ref.Type, ref.ID)
		}
		rows, err = tx.QueryContext(ctx, topologyOwners+topologyNodeSelect+" WHERE "+strings.Join(filters, " OR ")+" ORDER BY o.kind,o.id", args...)
		if err != nil {
			return out, err
		}
		found := make(map[domain.EntityRef]bool)
		for rows.Next() {
			var node domain.TopologyNode
			if err := rows.Scan(&node.Ref.Type, &node.Ref.ID, &node.Name, &node.RackName, &node.SiteName); err != nil {
				rows.Close()
				return out, err
			}
			node.Outside = !seeds[node.Ref]
			found[node.Ref] = true
			out.Nodes = append(out.Nodes, node)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return out, err
		}
		for _, ref := range peers {
			if !found[ref] {
				out.Nodes = append(out.Nodes, domain.TopologyNode{Ref: ref, Name: fmt.Sprintf("Missing %s #%d", ref.Type, ref.ID), Outside: true, Missing: true})
			}
		}
	}
	return out, tx.Commit()
}
