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

func TestExpiryChecksUseCallerCalendarDate(t *testing.T) {
	for _, tc := range []struct {
		name       string
		now        time.Time
		withinDays int
		included   string
		excluded   string
	}{
		{
			name:     "late night west of UTC",
			now:      time.Date(2026, 1, 1, 23, 30, 0, 0, time.FixedZone("UTC-5", -5*60*60)),
			included: "2026-01-01", excluded: "2026-01-02",
		},
		{
			name:     "early morning east of UTC",
			now:      time.Date(2026, 1, 1, 0, 30, 0, 0, time.FixedZone("UTC+14", 14*60*60)),
			included: "2026-01-01", excluded: "2026-01-02",
		},
		{
			name:       "lookahead crosses year boundary west of UTC",
			now:        time.Date(2026, 12, 31, 23, 30, 0, 0, time.FixedZone("UTC-5", -5*60*60)),
			withinDays: 1, included: "2027-01-01", excluded: "2027-01-02",
		},
		{
			name:       "lookahead crosses month boundary east of UTC",
			now:        time.Date(2026, 1, 31, 0, 30, 0, 0, time.FixedZone("UTC+14", 14*60*60)),
			withinDays: 1, included: "2026-02-01", excluded: "2026-02-02",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			certs := []Certificate{{ID: 1, ExpiresOn: tc.included}, {ID: 2, ExpiresOn: tc.excluded}}
			if got := ExpiringSoon(certs, tc.now, tc.withinDays); len(got) != 1 || got[0].ID != 1 {
				t.Errorf("certificate expiry = %+v, want only %s", got, tc.included)
			}
			hardware := []Hardware{{ID: 1, WarrantyEnd: tc.included}, {ID: 2, WarrantyEnd: tc.excluded}}
			if got := WarrantyExpiring(hardware, tc.now, tc.withinDays); len(got) != 1 || got[0].ID != 1 {
				t.Errorf("warranty expiry = %+v, want only %s", got, tc.included)
			}
			subscriptions := []Subscription{{ID: 1, RenewalDate: tc.included}, {ID: 2, RenewalDate: tc.excluded}}
			if got := RenewalsDue(subscriptions, tc.now, tc.withinDays); len(got) != 1 || got[0].ID != 1 {
				t.Errorf("renewal expiry = %+v, want only %s", got, tc.included)
			}
		})
	}
}
