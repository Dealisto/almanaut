package store

import (
	"reflect"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"gopkg.in/yaml.v3"
)

func TestExportImportPreservesLiteralTagPrefixes(t *testing.T) {
	for _, rawNames := range [][]string{
		{"##Prod"},
		{"##"},
		{"prod", "##Prod"},
		{"prod", "##Prod", "###Prod", "##", "##  Ops"},
	} {
		t.Run(rawNames[0], func(t *testing.T) {
			db := newTestDB(t)
			id, err := NewHostRepo(db).Create(domain.Host{Name: "tagged host", Type: "physical"})
			if err != nil {
				t.Fatal(err)
			}
			repo := NewTagRepo(db)
			for _, name := range rawNames {
				tag := domain.Tag{EntityType: "host", EntityID: id, Name: name}
				if err := tag.Validate(); err != nil {
					t.Fatal(err)
				}
				if err := repo.Add(tag); err != nil {
					t.Fatal(err)
				}
			}
			want, err := repo.List()
			if err != nil {
				t.Fatal(err)
			}
			var previous []byte
			for cycle := 0; cycle < 3; cycle++ {
				snap, err := Export(db)
				if err != nil {
					t.Fatal(err)
				}
				raw, err := yaml.Marshal(snap)
				if err != nil {
					t.Fatal(err)
				}
				if cycle > 0 && string(raw) != string(previous) {
					t.Fatalf("cycle %d changed the exported representation", cycle)
				}
				previous = raw
				var restored Snapshot
				if err := yaml.Unmarshal(raw, &restored); err != nil {
					t.Fatal(err)
				}
				if err := Import(db, restored); err != nil {
					t.Fatalf("cycle %d could not restore its own export: %v", cycle, err)
				}
				got, err := repo.List()
				if err != nil || !reflect.DeepEqual(got, want) {
					t.Fatalf("cycle %d changed stored tags: got %+v, want %+v, error %v", cycle, got, want, err)
				}
			}
		})
	}
}
