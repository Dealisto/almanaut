package web

import (
	"reflect"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func TestBulkStringEditPreservesLargeIntegerFields(t *testing.T) {
	const largeID int64 = 9007199254740993
	before := domain.Host{ID: largeID, Name: "before", Type: "physical", RackID: largeID + 2, RackPosition: 1, UHeight: 1}
	got, err := mergeField(before, "name", "after")
	if err != nil {
		t.Fatal(err)
	}
	want := before
	want.Name = "after"
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("bulk string edit changed unrelated fields: got %+v, want %+v", got, want)
	}
	port := domain.Port{ID: largeID, OwnerType: "host", OwnerID: largeID, NICID: largeID + 2, PeerPortID: largeID + 4, Name: "before"}
	updatedPort, err := mergeField(port, "name", "after")
	if err != nil {
		t.Fatal(err)
	}
	port.Name = "after"
	if !reflect.DeepEqual(updatedPort, port) {
		t.Fatalf("bulk port edit changed references: got %+v, want %+v", updatedPort, port)
	}
}
