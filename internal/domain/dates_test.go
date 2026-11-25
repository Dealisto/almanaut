package domain

import (
	"reflect"
	"testing"
	"time"
)

func TestExpiryChecksIncludeValidatedPaddedDates(t *testing.T) {
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	certs := []Certificate{
		{ID: 1, Subject: "later", ExpiresOn: " \t2026-11-24\n"},
		{ID: 2, Subject: "earlier", ExpiresOn: "2026-11-20"},
		{ID: 3, Subject: "future", ExpiresOn: " 2027-01-01 "},
	}
	for _, cert := range certs {
		if err := cert.Validate(); err != nil {
			t.Fatalf("test date must pass validation: %v", err)
		}
	}
	before := append([]Certificate(nil), certs...)
	got := ExpiringSoon(certs, now, 30)
	if len(got) != 2 || got[0].ID != 2 || got[1].ID != 1 {
		t.Fatalf("expiry selection/order = %+v, want earlier then later", got)
	}
	if !reflect.DeepEqual(certs, before) || got[1].ExpiresOn != before[0].ExpiresOn {
		t.Fatal("expiry projection must preserve source values")
	}
	if got := WarrantyExpiring([]Hardware{{Name: "switch", WarrantyEnd: " 2026-11-20 "}}, now, 30); len(got) != 1 {
		t.Fatalf("padded warranty date missing: %+v", got)
	}
	if got := RenewalsDue([]Subscription{{Name: "license", RenewalDate: " 2026-11-20 "}}, now, 30); len(got) != 1 {
		t.Fatalf("padded renewal date missing: %+v", got)
	}
}
