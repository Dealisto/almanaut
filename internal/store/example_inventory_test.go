package store

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestExampleInventoryImportsWithStrictSchema(t *testing.T) {
	f, err := os.Open(filepath.Join("..", "..", "examples", "inventory.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	decoder := yaml.NewDecoder(f)
	decoder.KnownFields(true)
	var sample Snapshot
	if err := decoder.Decode(&sample); err != nil {
		t.Fatalf("sample inventory must satisfy the import schema: %v", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		t.Fatalf("sample must contain exactly one YAML document: %v", err)
	}
	db := newTestDB(t)
	if err := Import(db, sample); err != nil {
		t.Fatalf("import sample: %v", err)
	}
	exported, err := Export(db)
	if err != nil {
		t.Fatalf("export sample: %v", err)
	}
	if len(exported.Networks) == 0 || len(exported.VLANs) == 0 {
		t.Fatal("sample must demonstrate a network linked to a VLAN")
	}
	network := exported.Networks[0]
	vlan := exported.VLANs[0]
	if network.VLANID != vlan.ID || vlan.VID != 1 {
		t.Fatalf("sample LAN lost its VLAN 1 link: network=%+v vlan=%+v", network, vlan)
	}
	if !reflect.DeepEqual(exported.Networks, sample.Networks) || !reflect.DeepEqual(exported.VLANs, sample.VLANs) {
		t.Fatal("sample network and VLAN data changed during import/export")
	}
	if len(sample.Relationships) == 0 || !reflect.DeepEqual(exported.Relationships, sample.Relationships) {
		t.Fatalf("sample relationships were lost or changed: %+v", exported.Relationships)
	}
	restored := newTestDB(t)
	if err := Import(restored, exported); err != nil {
		t.Fatalf("restore exported sample: %v", err)
	}
	roundTrip, err := Export(restored)
	if err != nil || !reflect.DeepEqual(roundTrip, exported) {
		t.Fatalf("sample export did not round trip intact: %v", err)
	}
}
