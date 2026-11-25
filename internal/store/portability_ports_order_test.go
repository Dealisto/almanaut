package store

import (
	"reflect"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func TestImportRepairsDanglingPortPeersBeforePairing(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		name := "incoming first"
		if reverse {
			name = "dangling first"
		}
		t.Run(name, func(t *testing.T) {
			db := newTestDB(t)
			ports := []domain.Port{
				{ID: 1, OwnerType: "host", OwnerID: 1, Name: "a", PeerPortID: 2},
				{ID: 2, OwnerType: "host", OwnerID: 1, Name: "b", PeerPortID: 99},
			}
			if reverse {
				ports[0], ports[1] = ports[1], ports[0]
			}
			before := append([]domain.Port(nil), ports...)
			if err := Import(db, Snapshot{Version: 1, Hosts: []domain.Host{{ID: 1, Name: "host", Type: "physical"}}, Ports: ports}); err != nil {
				t.Fatalf("repairable links rejected: %v", err)
			}
			got, err := NewPortRepo(db).List()
			if err != nil || len(got) != 2 {
				t.Fatalf("imported ports: %+v, %v", got, err)
			}
			for _, p := range got {
				if p.PeerPortID != 3-p.ID {
					t.Errorf("port %d peer = %d, want %d", p.ID, p.PeerPortID, 3-p.ID)
				}
			}
			if !reflect.DeepEqual(before, ports) {
				t.Error("import mutated caller's port list")
			}
		})
	}
}
