package web

import (
	"fmt"

	"github.com/Dealisto/almanaut/internal/store"
)

// querySpec exposes trusted SQL metadata from the same resource catalog that
// owns routes, labels and forms. Query values are bound separately by store.
func (rs resource[T]) querySpec() store.InventoryQuery {
	spec := rs.query
	spec.Type = rs.sing
	spec.Search = rs.search
	return spec
}
func inventoryQuery(table, label, order string, fields ...store.InventoryField) store.InventoryQuery {
	return store.InventoryQuery{Table: table, Label: label, Order: order, Fields: append([]store.InventoryField{{Label: "Name", Expression: label}}, fields...)}
}
func sqlField(label, expression string) store.InventoryField {
	return store.InventoryField{Label: label, Expression: expression}
}
func sqlReference(table, key, value string) string {
	return fmt.Sprintf("CASE WHEN e.%s=0 THEN '—' ELSE COALESCE((SELECT %s FROM %s r WHERE r.id=e.%s), '(unknown)') END", key, value, table, key)
}
func sqlRackPosition() string {
	return "CASE WHEN e.rack_id=0 THEN '—' ELSE printf('%d (%dU)',e.rack_position,e.u_height) END"
}
func sqlLiveness(typ string) string {
	return "COALESCE((SELECT CASE WHEN l.status='down' THEN CASE WHEN l.last_error='' THEN 'down' ELSE 'down · '||l.last_error END ELSE 'up · checked '||almanaut_minute(l.checked_at) END FROM liveness_state l WHERE l.entity_type='" + typ + "' AND l.entity_id=e.id),'—')"
}
func sqlOwner(alias string) string {
	return "COALESCE(CASE WHEN " + alias + ".owner_type='hardware' THEN (SELECT name FROM hardware WHERE id=" + alias + ".owner_id) ELSE (SELECT name FROM hosts WHERE id=" + alias + ".owner_id) END, " + alias + ".owner_type||':'||" + alias + ".owner_id||' (deleted)')"
}
func sqlNICHost() string {
	return "COALESCE((SELECT name FROM hosts WHERE id=e.host_id),'host:'||e.host_id||' (deleted)')"
}
func sqlNICName() string {
	return "CASE WHEN e.nic_id=0 THEN '' ELSE COALESCE((SELECT name FROM nics WHERE id=e.nic_id),'nic:'||e.nic_id||' (deleted)') END"
}
func sqlPortPeer() string {
	return "CASE WHEN e.peer_port_id=0 THEN '—' ELSE COALESCE((SELECT " + sqlOwner("peer") + "||' / '||peer.name FROM ports peer WHERE peer.id=e.peer_port_id),'port:'||e.peer_port_id||' (deleted)') END"
}

func sqlOwnedLabel(owner string) string {
	return "CASE WHEN (" + owner + ")='' THEN e.name ELSE (" + owner + ")||' / '||e.name END"
}
