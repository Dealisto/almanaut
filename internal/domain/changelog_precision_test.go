package domain

import "testing"

func TestDiffPreservesLargeRelationshipIDs(t *testing.T) {
	old := Host{ID: 1, RackID: 9007199254740992}
	updated := old
	updated.RackID++
	changes, err := Diff(old, updated)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Field != "rack_id" || changes[0].Old != "9007199254740992" || changes[0].New != "9007199254740993" {
		t.Fatalf("relationship change lost or rounded: %+v", changes)
	}
}
