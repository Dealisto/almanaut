package web

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestInvalidBooleanInputPreservesCertificate(t *testing.T) {
	for _, transport := range []string{"form", "csv"} {
		t.Run(transport, func(t *testing.T) {
			srv, db := newTestServerDB(t)
			repo := store.NewCertificateRepo(db)
			id, err := repo.Create(domain.Certificate{Subject: "original.example", ExpiresOn: "2030-01-01", AutoRenew: true})
			if err != nil {
				t.Fatal(err)
			}
			var body string
			if transport == "form" {
				rec := postForm(t, srv, "/certificates/"+itoa(id), url.Values{"subject": {"changed.example"}, "expires_on": {"2030-01-01"}, "auto_renew": {"treu"}})
				body = rec.Body.String()
			} else {
				rec := uploadCSV(t, srv, "certificate", "id,subject,expires_on,auto_renew\n,new.example,2030-01-01,true\n"+itoa(id)+",changed.example,2030-01-01,treu\n")
				body = rec.Body.String()
			}
			if !strings.Contains(body, "auto_renew must be a valid boolean") {
				t.Fatalf("invalid boolean was not explained: %s", body)
			}
			items, err := repo.List()
			if err != nil || len(items) != 1 || items[0].Subject != "original.example" || !items[0].AutoRenew {
				t.Fatalf("invalid boolean changed inventory: %+v err=%v", items, err)
			}
		})
	}
}

func TestCSVFramingErrorIsShownWithoutWriting(t *testing.T) {
	srv, db := newTestServerDB(t)
	rec := uploadCSV(t, srv, "host", "name,type\nvalid,physical\n\"unterminated,vm\n")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "invalid CSV:") || !strings.Contains(rec.Body.String(), "line 3") {
		t.Fatalf("CSV syntax error status=%d body=%s", rec.Code, rec.Body.String())
	}
	items, err := store.NewHostRepo(db).List()
	if err != nil || len(items) != 0 {
		t.Fatalf("malformed CSV changed inventory: %+v err=%v", items, err)
	}
}

type failingCSVReader struct{ err error }

func (r failingCSVReader) Read([]byte) (int, error) { return 0, r.err }

func TestCSVReadFailureRemainsInfrastructureError(t *testing.T) {
	rs, deps := hostResourceForTest(t)
	want := errors.New("upload read failed")
	_, _, rowErrors, err := rs.importCSV(deps, io.MultiReader(strings.NewReader("name,type\n"), failingCSVReader{want}), "tester")
	if !errors.Is(err, want) || len(rowErrors) != 0 {
		t.Fatalf("read failure masked as input validation: rowErrors=%v err=%v", rowErrors, err)
	}
}

func TestBooleanInputPreservesSupportedValues(t *testing.T) {
	rs := resource[domain.Certificate]{parse: parseCertificate}
	for _, tc := range []struct {
		raw  string
		want bool
	}{
		{"on", true}, {" TRUE ", true}, {"1", true}, {"yes", true}, {"Y", true},
		{"", false}, {"off", false}, {" FALSE ", false}, {"0", false}, {"no", false}, {"N", false},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			item, err := rs.parseInput(func(field string) string {
				if field == "auto_renew" {
					return tc.raw
				}
				return ""
			}, 0)
			if err != nil || item.AutoRenew != tc.want {
				t.Fatalf("input=%q auto_renew=%v err=%v, want %v", tc.raw, item.AutoRenew, err, tc.want)
			}
		})
	}
}
