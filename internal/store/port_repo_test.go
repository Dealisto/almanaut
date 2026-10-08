package store

import (
	"errors"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func TestPortRepoRoundTrip(t *testing.T) {
	db := newTestDB(t)
	hwID, err := NewHardwareRepo(db).Create(domain.Hardware{Name: "switch01", Kind: "switch"})
	if err != nil {
		t.Fatalf("create hardware: %v", err)
	}
	repo := NewPortRepo(db)
	id, err := repo.Create(domain.Port{OwnerType: "hardware", OwnerID: hwID, Name: "Port 1", MAC: "aa:bb:cc:dd:ee:ff", MgmtOnly: true, Notes: "uplink"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := repo.Get(id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.OwnerType != "hardware" || got.OwnerID != hwID || got.Name != "Port 1" || got.MAC != "aa:bb:cc:dd:ee:ff" || !got.MgmtOnly || got.Notes != "uplink" {
		t.Fatalf("fields not persisted: %+v", got)
	}
	if got.OwnerName != "switch01" {
		t.Fatalf("OwnerName not derived: %q", got.OwnerName)
	}
	got.Name = "Port 1 renamed"
	if err := repo.Update(got); err != nil {
		t.Fatalf("update: %v", err)
	}
	after, _ := repo.Get(id)
	if after.Name != "Port 1 renamed" {
		t.Fatalf("update not persisted: %q", after.Name)
	}
	if n, err := repo.Count(); err != nil || n != 1 {
		t.Fatalf("count = %d, %v", n, err)
	}
	if err := repo.Delete(id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.Get(id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestPortRepoUpdateMissingIsNotFound(t *testing.T) {
	db := newTestDB(t)
	err := NewPortRepo(db).Update(domain.Port{ID: 99, OwnerType: "host", OwnerID: 1, Name: "x"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// Connecting one port to another records the link on both of them.
func TestPortRepoConnectsBothWays(t *testing.T) {
	db := newTestDB(t)
	hostID, err := NewHostRepo(db).Create(domain.Host{Name: "server1"})
	if err != nil {
		t.Fatalf("create host: %v", err)
	}
	hwID, err := NewHardwareRepo(db).Create(domain.Hardware{Name: "switch01", Kind: "switch"})
	if err != nil {
		t.Fatalf("create hardware: %v", err)
	}
	nicID, err := NewNICRepo(db).Create(domain.NIC{HostID: hostID, Name: "onboard", Kind: "onboard"})
	if err != nil {
		t.Fatalf("create nic: %v", err)
	}
	repo := NewPortRepo(db)
	swPort, err := repo.Create(domain.Port{OwnerType: "hardware", OwnerID: hwID, Name: "Port 12"})
	if err != nil {
		t.Fatalf("create switch port: %v", err)
	}
	hostPort, err := repo.Create(domain.Port{OwnerType: "host", OwnerID: hostID, NICID: nicID, Name: "eth0", PeerPortID: swPort})
	if err != nil {
		t.Fatalf("create host port: %v", err)
	}

	hp, err := repo.Get(hostPort)
	if err != nil {
		t.Fatalf("get host port: %v", err)
	}
	if hp.PeerPortID != swPort || hp.PeerLabel != "switch01 / Port 12" {
		t.Fatalf("forward peer not resolved: PeerPortID=%d PeerLabel=%q", hp.PeerPortID, hp.PeerLabel)
	}
	if hp.NICName != "onboard" {
		t.Fatalf("NICName not derived: %q", hp.NICName)
	}

	sp, err := repo.Get(swPort)
	if err != nil {
		t.Fatalf("get switch port: %v", err)
	}
	if sp.PeerPortID != hostPort || sp.PeerLabel != "server1 / eth0" {
		t.Fatalf("reverse peer not stored: PeerPortID=%d PeerLabel=%q", sp.PeerPortID, sp.PeerLabel)
	}
}

// Deleting a port must clear dangling peer_port_id pointers on other ports.
func TestPortRepoDeleteClearsPeerPointers(t *testing.T) {
	db := newTestDB(t)
	repo := NewPortRepo(db)
	a, err := repo.Create(domain.Port{OwnerType: "hardware", OwnerID: 1, Name: "Port 1"})
	if err != nil {
		t.Fatalf("create a: %v", err)
	}
	b, err := repo.Create(domain.Port{OwnerType: "host", OwnerID: 1, Name: "eth0", PeerPortID: a})
	if err != nil {
		t.Fatalf("create b: %v", err)
	}
	if err := repo.Delete(a); err != nil {
		t.Fatalf("delete: %v", err)
	}
	got, err := repo.Get(b)
	if err != nil {
		t.Fatalf("get b: %v", err)
	}
	if got.PeerPortID != 0 || got.PeerLabel != "" {
		t.Fatalf("peer pointer should be cleared, got PeerPortID=%d PeerLabel=%q", got.PeerPortID, got.PeerLabel)
	}
}

// "Port 2" must sort before "Port 10" (length-then-name natural order).
func TestPortRepoListNaturalOrder(t *testing.T) {
	db := newTestDB(t)
	repo := NewPortRepo(db)
	for _, name := range []string{"Port 10", "Port 2", "Port 1"} {
		if _, err := repo.Create(domain.Port{OwnerType: "hardware", OwnerID: 1, Name: name}); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	items, err := repo.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 3 || items[0].Name != "Port 1" || items[1].Name != "Port 2" || items[2].Name != "Port 10" {
		names := []string{}
		for _, p := range items {
			names = append(names, p.Name)
		}
		t.Fatalf("unexpected order: %v", names)
	}
}

// A dangling owner reference is surfaced, not hidden.
func TestPortRepoMarksDeletedOwner(t *testing.T) {
	db := newTestDB(t)
	repo := NewPortRepo(db)
	id, err := repo.Create(domain.Port{OwnerType: "host", OwnerID: 42, Name: "eth0"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := repo.Get(id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.OwnerName != "host:42 (deleted)" {
		t.Fatalf("dangling owner should be marked, got %q", got.OwnerName)
	}
}

// peers returns each port's stored peer_port_id, keyed by port id.
func peers(t *testing.T, repo *PortRepo) map[int64]int64 {
	t.Helper()
	items, err := repo.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	out := map[int64]int64{}
	for _, p := range items {
		out[p.ID] = p.PeerPortID
	}
	return out
}

// A port already cabled to a third port cannot be taken over silently.
func TestPortRepoRefusesAlreadyConnectedPeer(t *testing.T) {
	db := newTestDB(t)
	repo := NewPortRepo(db)
	a, _ := repo.Create(domain.Port{OwnerType: "hardware", OwnerID: 1, Name: "Port 1"})
	b, err := repo.Create(domain.Port{OwnerType: "host", OwnerID: 1, Name: "eth0", PeerPortID: a})
	if err != nil {
		t.Fatalf("create b: %v", err)
	}
	_, err = repo.Create(domain.Port{OwnerType: "host", OwnerID: 2, Name: "eth0", PeerPortID: a})
	var ce *ConstraintError
	if !errors.As(err, &ce) {
		t.Fatalf("connecting to a taken port: got %v, want a ConstraintError", err)
	}
	// Nor can a third port claim b, the other end of the same cable.
	c, _ := repo.Create(domain.Port{OwnerType: "host", OwnerID: 3, Name: "eth0"})
	if err := repo.Update(domain.Port{ID: c, OwnerType: "host", OwnerID: 3, Name: "eth0", PeerPortID: b}); !errors.As(err, &ce) {
		t.Fatalf("connecting to b, which is cabled to a: got %v, want a ConstraintError", err)
	}
	if got := peers(t, repo); got[a] != b || got[b] != a || got[c] != 0 {
		t.Fatalf("a refused connection must change nothing, got %v", got)
	}
}

// Re-cabling a port releases its old peer; clearing its peer disconnects both.
func TestPortRepoRecableAndDisconnect(t *testing.T) {
	db := newTestDB(t)
	repo := NewPortRepo(db)
	a, _ := repo.Create(domain.Port{OwnerType: "hardware", OwnerID: 1, Name: "Port 1"})
	b, _ := repo.Create(domain.Port{OwnerType: "hardware", OwnerID: 1, Name: "Port 2"})
	host, err := repo.Create(domain.Port{OwnerType: "host", OwnerID: 1, Name: "eth0", PeerPortID: a})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// Moving host's cable from a to b frees a.
	if err := repo.Update(domain.Port{ID: host, OwnerType: "host", OwnerID: 1, Name: "eth0", PeerPortID: b}); err != nil {
		t.Fatalf("re-cable: %v", err)
	}
	if got := peers(t, repo); got[host] != b || got[b] != host || got[a] != 0 {
		t.Fatalf("after re-cable got %v", got)
	}
	// Disconnect from the other end: b's edit with no peer frees host too.
	if err := repo.Update(domain.Port{ID: b, OwnerType: "hardware", OwnerID: 1, Name: "Port 2"}); err != nil {
		t.Fatalf("disconnect: %v", err)
	}
	if got := peers(t, repo); got[host] != 0 || got[b] != 0 {
		t.Fatalf("after disconnect got %v", got)
	}
}

// Saving a port unchanged (as an API client does after a GET) keeps its link.
func TestPortRepoUpdateUnchangedKeepsLink(t *testing.T) {
	db := newTestDB(t)
	repo := NewPortRepo(db)
	a, _ := repo.Create(domain.Port{OwnerType: "hardware", OwnerID: 1, Name: "Port 1"})
	b, _ := repo.Create(domain.Port{OwnerType: "host", OwnerID: 1, Name: "eth0", PeerPortID: a})
	for _, id := range []int64{a, b} {
		p, err := repo.Get(id)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if err := repo.Update(p); err != nil {
			t.Fatalf("update %d unchanged: %v", id, err)
		}
	}
	if got := peers(t, repo); got[a] != b || got[b] != a {
		t.Fatalf("unchanged saves must keep the link, got %v", got)
	}
}

func TestPortRepoRefusesMissingPeer(t *testing.T) {
	db := newTestDB(t)
	_, err := NewPortRepo(db).Create(domain.Port{OwnerType: "host", OwnerID: 1, Name: "eth0", PeerPortID: 42})
	var ce *ConstraintError
	if !errors.As(err, &ce) {
		t.Fatalf("got %v, want a ConstraintError", err)
	}
}

// The schema itself refuses two ports naming the same peer.
func TestPortsPeerIsUniqueInSchema(t *testing.T) {
	db := newTestDB(t)
	if _, err := db.Exec(`INSERT INTO ports (owner_type, owner_id, name, peer_port_id) VALUES ('host', 1, 'a', 9), ('host', 2, 'b', 9)`); err == nil {
		t.Fatal("two ports with the same peer_port_id were accepted")
	}
}
