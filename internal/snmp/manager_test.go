package snmp

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/store"
)

type fakeScanner struct {
	value Observation
	err   error
}

func (f *fakeScanner) Scan(context.Context) (Observation, error) { return f.value, f.err }
func TestSNMPManagerPreservesSuccessfulObservationAndRedactsErrors(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "inventory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := store.Migrate(db, ""); err != nil {
		t.Fatal(err)
	}
	f := &fakeScanner{value: Observation{SystemName: "original"}}
	m := NewManager(db, "switch", f)
	if _, err := m.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.err = errors.New("community SECRET")
	if _, err := m.Scan(context.Background()); err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("scan error %v", err)
	}
	state, err := Load(context.Background(), db, "switch")
	if err != nil || state.Observation.SystemName != "original" || state.ObservedAt == "" || state.LastError == "" || strings.Contains(state.LastError, "SECRET") {
		t.Fatalf("state %+v %v", state, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.Scan(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation %v", err)
	}
	f.err = nil
	f.value.SystemName = "updated"
	if err := m.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	state, err = Load(context.Background(), db, "switch")
	if err != nil || state.Observation.SystemName != "updated" || state.LastError != "" {
		t.Fatalf("recovery %+v %v", state, err)
	}
}

type cancelingScanner struct {
	cancel context.CancelFunc
	value  Observation
}

func (f cancelingScanner) Scan(ctx context.Context) (Observation, error) {
	f.cancel()
	return f.value, ctx.Err()
}
func TestSNMPManagerPersistsCanceledStartedAndOversizeFailures(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "inventory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := store.Migrate(db, ""); err != nil {
		t.Fatal(err)
	}
	f := &fakeScanner{value: Observation{SystemName: "retained"}}
	m := NewManager(db, "switch", f, "config-a")
	if _, err := m.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	original, _ := Load(context.Background(), db, "switch", "config-a")
	ctx, cancel := context.WithCancel(context.Background())
	m.scanner = cancelingScanner{cancel: cancel}
	if _, err := m.Scan(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("scan %v", err)
	}
	canceled, err := Load(context.Background(), db, "switch", "config-a")
	if err != nil || canceled.ObservedAt != original.ObservedAt || canceled.Observation.SystemName != "retained" || !strings.Contains(canceled.LastError, "canceled") {
		t.Fatalf("failure disappeared: %+v %v", canceled, err)
	}
	// An already canceled queued caller must not overwrite attempt metadata.
	if _, err := m.Scan(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	unchanged, _ := Load(context.Background(), db, "switch", "config-a")
	if unchanged.AttemptedAt != canceled.AttemptedAt {
		t.Fatal("unstarted scan wrote attempt")
	}
	m.scanner = &fakeScanner{value: Observation{Description: strings.Repeat("x", (2<<20)+1)}}
	if _, err := m.Scan(context.Background()); err == nil {
		t.Fatal("oversized snapshot accepted")
	}
	oversized, _ := Load(context.Background(), db, "switch", "config-a")
	if oversized.LastError == "" || oversized.Observation.SystemName != "retained" || oversized.AttemptedAt == canceled.AttemptedAt {
		t.Fatalf("oversize lost status: %+v", oversized)
	}
}
func TestSNMPManagerInvalidatesChangedSourceConfiguration(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "inventory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := store.Migrate(db, ""); err != nil {
		t.Fatal(err)
	}
	scanner := &fakeScanner{value: Observation{SystemName: "same-default-name", LLDPComplete: true, Neighbors: []Neighbor{{LocalID: "port1", ChassisID: "same-peer", PortID: "port2"}}}}
	old := NewManager(db, "switch", scanner, "endpoint-a")
	if _, err := old.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	changed, err := Load(context.Background(), db, "switch", "endpoint-b")
	if err != nil || !changed.ConfigurationChanged || changed.ObservedAt != "" || changed.Observation.SystemName != "" || len(changed.Observation.Neighbors) != 0 {
		t.Fatalf("stale source visible %+v %v", changed, err)
	}
	current := NewManager(db, "switch", scanner, "endpoint-b")
	scanner.err = errors.New("unreachable")
	if _, err := current.Scan(context.Background()); err == nil {
		t.Fatal("expected failure")
	}
	changed, _ = Load(context.Background(), db, "switch", "endpoint-b")
	if !changed.ConfigurationChanged || changed.Observation.LLDPComplete {
		t.Fatal("failed scan relabeled old data")
	}
	scanner.err = nil
	if _, err := current.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	fresh, _ := Load(context.Background(), db, "switch", "endpoint-b")
	if fresh.ConfigurationChanged || fresh.ObservedAt == "" || fresh.Observation.SystemName != "same-default-name" {
		t.Fatalf("fresh state %+v", fresh)
	}
}

func TestSNMPFirstFailedScanKeepsCurrentConfigurationIdentity(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "inventory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := store.Migrate(db, ""); err != nil {
		t.Fatal(err)
	}
	m := NewManager(db, "switch", &fakeScanner{err: errors.New("unreachable")}, "configured-endpoint")
	if _, err := m.Scan(context.Background()); err == nil {
		t.Fatal("expected failure")
	}
	snapshot, err := Load(context.Background(), db, "switch", "configured-endpoint")
	if err != nil || snapshot.ConfigurationChanged || snapshot.ObservedAt != "" || snapshot.LastError == "" || snapshot.AttemptedAt == "" {
		t.Fatalf("first failure misclassified: %+v %v", snapshot, err)
	}
}
