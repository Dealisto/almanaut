package web

import (
	"strings"
	"testing"
)

func TestImportCSVRejectsDuplicateHeadersBeforeWriting(t *testing.T) {
	for _, header := range []string{"name,type,name", "name,type, name ", "\ufeffname,type,name"} {
		t.Run(header, func(t *testing.T) {
			rs, deps := hostResourceForTest(t)
			created, updated, rowErrs, err := rs.importCSV(deps, strings.NewReader(header+"\nfirst,physical,second\n"), "tester")
			if err != nil {
				t.Fatal(err)
			}
			if created != 0 || updated != 0 || len(rowErrs) != 1 || !strings.Contains(rowErrs[0], `duplicate column "name"`) {
				t.Fatalf("duplicate header: created=%d updated=%d errors=%v", created, updated, rowErrs)
			}
			items, err := rs.repo.List()
			if err != nil || len(items) != 0 {
				t.Fatalf("invalid header wrote inventory: %+v, %v", items, err)
			}
		})
	}
}
