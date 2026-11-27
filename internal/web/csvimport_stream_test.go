package web

import (
	"context"
	"encoding/csv"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func TestCSVStreamingPreservesSyntaxErrorPrecedence(t *testing.T) {
	for _, prefix := range []string{
		"unknown\nvalue\n",
		"name,type,name\nfirst,vm,second\n",
		"name,type\n,vm\n",
		"id,name,type\nnot-an-id,x,vm\n",
		"id,name,type\n999,x,vm\n",
	} {
		t.Run(strings.Split(prefix, "\n")[0]+prefix, func(t *testing.T) {
			rs, deps := hostResourceForTest(t)
			doc := prefix + "\"unterminated\n"
			reader := csv.NewReader(strings.NewReader(doc))
			reader.FieldsPerRecord = -1
			_, syntax := reader.ReadAll()
			if syntax == nil {
				t.Fatal("fixture must contain a syntax error")
			}
			created, updated, rowErrs, err := rs.importCSV(deps, strings.NewReader(doc), "tester")
			if err != nil || created != 0 || updated != 0 || !reflect.DeepEqual(rowErrs, []string{"invalid CSV: " + syntax.Error()}) {
				t.Fatalf("syntax precedence changed: created=%d updated=%d rows=%v error=%v", created, updated, rowErrs, err)
			}
			if items, err := rs.repo.List(); err != nil || len(items) != 0 {
				t.Fatalf("syntax failure wrote inventory: %+v %v", items, err)
			}
		})
	}
}

func TestCSVStreamingPreservesReaderFailurePrecedence(t *testing.T) {
	for _, prefix := range []string{"unknown\nvalue\n", "name,type,name\nx,vm,y\n", "name,type\n,vm\n"} {
		t.Run(prefix, func(t *testing.T) {
			rs, deps := hostResourceForTest(t)
			want := errors.New("upload interrupted")
			reader := io.MultiReader(strings.NewReader(prefix), failingCSVReader{err: want})
			_, _, rowErrs, err := rs.importCSV(deps, reader, "tester")
			if !errors.Is(err, want) || len(rowErrs) != 0 {
				t.Fatalf("reader failure masked: rows=%v error=%v", rowErrs, err)
			}
		})
	}
}

func TestCSVStreamingPreservesReadFailuresBeforeDatabaseFailures(t *testing.T) {
	for _, syntax := range []bool{false, true} {
		t.Run(map[bool]string{false: "reader", true: "syntax"}[syntax], func(t *testing.T) {
			rs, deps := hostResourceForTest(t)
			if err := deps.db.Close(); err != nil {
				t.Fatal(err)
			}
			prefix := "id,name,type\n1,host,vm\n"
			want := errors.New("upload interrupted")
			var reader io.Reader = io.MultiReader(strings.NewReader(prefix), failingCSVReader{err: want})
			if syntax {
				reader = strings.NewReader(prefix + "\"unterminated\n")
			}
			_, _, rowErrs, err := rs.importCSV(deps, reader, "tester")
			if syntax {
				if err != nil || len(rowErrs) != 1 || !strings.HasPrefix(rowErrs[0], "invalid CSV:") {
					t.Fatalf("database failure masked syntax: rows=%v error=%v", rowErrs, err)
				}
			} else if !errors.Is(err, want) || len(rowErrs) != 0 {
				t.Fatalf("database failure masked reader: rows=%v error=%v", rowErrs, err)
			}
		})
	}
}

func TestCSVStreamingPreservesRecordNumbersAndAllRowErrors(t *testing.T) {
	rs, deps := hostResourceForTest(t)
	doc := "\ufeffname,type,notes\r\n\r\nfirst,vm,\"first line\nsecond line\"\r\n,vm,note\r\nshort,vm\r\nthird,vm,note\r\n,vm,last\r\n"
	_, _, rowErrs, err := rs.importCSV(deps, strings.NewReader(doc), "tester")
	want := []string{"row 3: name is required", "row 4: has 2 columns, expected 3", "row 6: name is required"}
	if err != nil || !reflect.DeepEqual(rowErrs, want) {
		t.Fatalf("record diagnostics changed: rows=%v error=%v", rowErrs, err)
	}
	if items, err := rs.repo.List(); err != nil || len(items) != 0 {
		t.Fatalf("row errors wrote inventory: %+v %v", items, err)
	}
}

type csvObservedReader struct {
	parts  []string
	before func(int)
	next   int
}

func (r *csvObservedReader) Read(p []byte) (int, error) {
	r.before(r.next)
	if r.next == len(r.parts) {
		return 0, io.EOF
	}
	n := copy(p, r.parts[r.next])
	r.parts[r.next] = r.parts[r.next][n:]
	if len(r.parts[r.next]) == 0 {
		r.next++
	}
	return n, nil
}

func TestCSVValidatesWhileReadingButWritesOnlyAfterEOF(t *testing.T) {
	rs, deps := hostResourceForTest(t)
	parsed := 0
	rs.parse = func(get func(string) string, id int64) domain.Host {
		parsed++
		return parseHost(get, id)
	}
	reader := &csvObservedReader{parts: []string{"name,type\n", "first,vm\n", "second,vm\n"}}
	reader.before = func(next int) {
		if next >= 2 && parsed != next-1 {
			t.Fatalf("row parsing waited for the whole file: read=%d parsed=%d", next, parsed)
		}
		if items, err := rs.repo.List(); err != nil || len(items) != 0 {
			t.Fatalf("inventory changed before EOF: %+v %v", items, err)
		}
	}
	created, updated, rowErrs, err := rs.importCSV(deps, reader, "tester")
	if err != nil || len(rowErrs) != 0 || created != 2 || updated != 0 {
		t.Fatalf("streamed import failed: created=%d updated=%d rows=%v error=%v", created, updated, rowErrs, err)
	}
	items, err := rs.repo.List()
	if err != nil || len(items) != 2 || items[0].Name != "first" || items[1].Name != "second" {
		t.Fatalf("reused record corrupted imported entities: %+v %v", items, err)
	}
}

func TestCSVStreamingCancellationAfterHeaderError(t *testing.T) {
	rs, deps := hostResourceForTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader := &csvObservedReader{parts: []string{"unknown\n", "value\n"}}
	reader.before = func(next int) {
		if next == 1 {
			cancel()
		}
	}
	_, _, rowErrs, err := rs.importCSVContext(ctx, deps, reader, "tester")
	if !errors.Is(err, context.Canceled) || len(rowErrs) != 0 {
		t.Fatalf("invalid header hid cancellation: rows=%v error=%v", rowErrs, err)
	}
}
