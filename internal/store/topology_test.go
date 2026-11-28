package store

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func TestTopologyScopesParallelCablesAndMissingOwners(t *testing.T) {
	db := newTestDB(t)
	for _, statement := range []string{
		`INSERT INTO sites(id,name) VALUES(1,'Main'),(2,'Other')`,
		`INSERT INTO locations(id,name,site_id) VALUES(1,'Room',1),(2,'Room',2)`,
		`INSERT INTO racks(id,name,location_id) VALUES(1,'Rack A',1),(2,'Rack B',2)`,
		`INSERT INTO hosts(id,name,type,rack_id) VALUES(1,'server','physical',1),(2,'remote','physical',2),(3,'unplaced','physical',0)`,
		`INSERT INTO hardware(id,name,rack_id) VALUES(1,'switch',1)`,
		`INSERT INTO ports(id,owner_type,owner_id,name,peer_port_id) VALUES
 (1,'host',1,'eth0',2),(2,'hardware',1,'one',1),
 (3,'host',1,'eth1',4),(4,'hardware',1,'two',3),
 (5,'hardware',1,'uplink',6),(6,'host',2,'eth0',5),
 (7,'host',2,'loop',8),(8,'host',1,'loop',7),
 (9,'host',1,'orphan-owner',10),(10,'host',999,'old',9),
 (11,'host',1,'missing-peer',999),
 (12,'host',1,'loop-a',13),(13,'host',1,'loop-b',12)`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	graph, err := ReadTopology(context.Background(), db, domain.TopologyScope{SiteID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Nodes) != 4 || len(graph.Cables) != 6 || graph.Broken != 1 || graph.Truncated {
		t.Fatalf("unexpected topology: %+v", graph)
	}
	seen := map[domain.EntityRef]domain.TopologyNode{}
	for _, node := range graph.Nodes {
		seen[node.Ref] = node
	}
	if !seen[domain.EntityRef{Type: "host", ID: 2}].Outside || !seen[domain.EntityRef{Type: "host", ID: 999}].Missing {
		t.Fatalf("peer classification: %+v", seen)
	}
	if seen[domain.EntityRef{Type: "host", ID: 1}].SiteName != "Main" {
		t.Fatal("site hierarchy not resolved")
	}
	graph, err = ReadTopology(context.Background(), db, domain.TopologyScope{Unassigned: true})
	if err != nil || len(graph.Nodes) != 1 || graph.Nodes[0].Name != "unplaced" {
		t.Fatalf("unassigned: %+v %v", graph, err)
	}
	graph, err = ReadTopology(context.Background(), db, domain.TopologyScope{Device: domain.EntityRef{Type: "host", ID: 2}})
	if err != nil || len(graph.Nodes) != 3 || len(graph.Cables) != 2 {
		t.Fatalf("one-hop scope: %+v %v", graph, err)
	}
	if _, err = ReadTopology(context.Background(), db, domain.TopologyScope{Device: domain.EntityRef{Type: "host", ID: 500}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing focus: %v", err)
	}
	for _, scope := range []domain.TopologyScope{{SiteID: 999}, {RackID: 999}} {
		if _, err := ReadTopology(context.Background(), db, scope); !errors.Is(err, ErrNotFound) {
			t.Fatalf("unknown scope: %v", err)
		}
	}
}

func TestTopologyBoundsAndCancelledReads(t *testing.T) {
	db := newTestDB(t)
	for i := 1; i <= 130; i++ {
		if _, err := db.Exec(`INSERT INTO hardware(id,name) VALUES(?,?)`, i, fmt.Sprintf("switch %03d", i)); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO sites(id,name) VALUES(?,?)`, i, fmt.Sprintf("site %03d", i)); err != nil {
			t.Fatal(err)
		}
	}
	graph, err := ReadTopology(context.Background(), db, domain.TopologyScope{})
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Nodes) != TopologySeedLimit || !graph.Truncated || len(graph.Sites) != 100 || !graph.ChoicesTruncated {
		t.Fatalf("limits not preserved: %+v", graph)
	}
	graph, err = ReadTopology(context.Background(), db, domain.TopologyScope{SiteID: 130})
	if err != nil || graph.Sites[0].ID != 130 {
		t.Fatalf("selected scope excluded from choices: %+v %v", graph.Sites, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ReadTopology(ctx, db, domain.TopologyScope{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled read: %v", err)
	}
}

func TestTopologyNodeAndCableBudgets(t *testing.T) {
	db := newTestDB(t)
	if _, err := db.Exec(`INSERT INTO hosts(id,name,type) VALUES(1,'hub','physical')`); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 110; i++ {
		if _, err := db.Exec(`INSERT INTO hardware(id,name) VALUES(?,?)`, i, fmt.Sprintf("peer%d", i)); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO ports(id,owner_type,owner_id,name,peer_port_id) VALUES(?,'host',1,'out',?),(?,'hardware',?,'in',?)`, i*2-1, i*2, i*2, i, i*2-1); err != nil {
			t.Fatal(err)
		}
	}
	graph, err := ReadTopology(context.Background(), db, domain.TopologyScope{Device: domain.EntityRef{Type: "host", ID: 1}})
	if err != nil || len(graph.Nodes) != TopologyNodeLimit || len(graph.Cables) != 99 || !graph.Truncated {
		t.Fatalf("node budget: nodes=%d cables=%d limited=%v error=%v", len(graph.Nodes), len(graph.Cables), graph.Truncated, err)
	}
	if _, err := db.Exec(`DELETE FROM ports`); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 210; i++ {
		if _, err := db.Exec(`INSERT INTO ports(id,owner_type,owner_id,name,peer_port_id) VALUES(?,'host',1,'out',?),(?,'hardware',1,'in',?)`, i*2-1, i*2, i*2, i*2-1); err != nil {
			t.Fatal(err)
		}
	}
	graph, err = ReadTopology(context.Background(), db, domain.TopologyScope{Device: domain.EntityRef{Type: "host", ID: 1}})
	if err != nil || len(graph.Nodes) != 2 || len(graph.Cables) != TopologyCableLimit || !graph.Truncated {
		t.Fatalf("cable budget: nodes=%d cables=%d limited=%v error=%v", len(graph.Nodes), len(graph.Cables), graph.Truncated, err)
	}
}
