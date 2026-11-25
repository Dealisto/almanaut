package web

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

const defaultPageSize = 50
const maxPageSize = 100

type pagination struct {
	Page, PerPage, Total, Pages, From, To int
	PreviousURL, NextURL, FirstURL        string
}

func parsePagination(req *http.Request) (pagination, error) {
	p := pagination{Page: 1, PerPage: defaultPageSize}
	for key, target := range map[string]*int{"page": &p.Page, "per_page": &p.PerPage} {
		if raw := req.URL.Query().Get(key); req.URL.Query().Has(key) {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 {
				return p, fmt.Errorf("%s must be a positive integer", key)
			}
			*target = n
		}
	}
	if p.PerPage > maxPageSize {
		return p, fmt.Errorf("per_page must not exceed %d", maxPageSize)
	}
	if p.Page-1 > int(^uint(0)>>1)/p.PerPage {
		return p, fmt.Errorf("page is too large")
	}
	return p, nil
}
func (p pagination) offset() int { return (p.Page - 1) * p.PerPage }
func (p *pagination) finish(req *http.Request, total int) {
	p.Total = total
	p.Pages = max(1, (total+p.PerPage-1)/p.PerPage)
	if p.offset() < total {
		p.From = p.offset() + 1
		p.To = min(total, p.offset()+p.PerPage)
	}
	link := func(page int) string {
		q := req.URL.Query()
		q.Set("page", strconv.Itoa(page))
		q.Set("per_page", strconv.Itoa(p.PerPage))
		return req.URL.EscapedPath() + "?" + q.Encode()
	}
	p.FirstURL = link(1)
	if p.Page > 1 {
		p.PreviousURL = link(min(p.Page-1, p.Pages))
	}
	if p.Page < p.Pages {
		p.NextURL = link(p.Page + 1)
	}
}
func (p pagination) headers(w http.ResponseWriter) {
	w.Header().Set("X-Total-Count", strconv.Itoa(p.Total))
	w.Header().Set("X-Page", strconv.Itoa(p.Page))
	w.Header().Set("X-Per-Page", strconv.Itoa(p.PerPage))
	links := []string{"<" + p.FirstURL + ">; rel=\"first\""}
	if p.PreviousURL != "" {
		links = append(links, "<"+p.PreviousURL+">; rel=\"prev\"")
	}
	if p.NextURL != "" {
		links = append(links, "<"+p.NextURL+">; rel=\"next\"")
	}
	w.Header().Set("Link", strings.Join(links, ", "))
}
func savedQuery(q url.Values) string { q.Del("page"); return q.Encode() }

type inventoryPage[T any] struct {
	Items    []T
	Values   map[int64][]domain.CustomFieldValue
	Controls listControls
}

func (rs resource[T]) listPage(req *http.Request, d handlerDeps, p pagination, controls bool) (inventoryPage[T], error) {
	q := req.URL.Query()
	ctrl := listControls{BasePath: rs.basePath(), Type: rs.sing, CurrentQuery: savedQuery(req.URL.Query()), Query: strings.TrimSpace(q.Get("q")), SetFields: rs.stringFields(), Sort: strings.TrimSpace(q.Get("sort")), Dir: strings.TrimSpace(q.Get("dir")), Tag: strings.TrimSpace(q.Get("tag")), FilterField: strings.TrimSpace(q.Get("field")), FilterValue: strings.TrimSpace(q.Get("value"))}
	if ctrl.Dir != "desc" {
		ctrl.Dir = "asc"
	}
	result := inventoryPage[T]{Items: []T{}, Controls: ctrl}
	tx, err := d.db.BeginTx(req.Context(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	cf := d.customFields.WithTx(tx)
	defs, err := cf.ListDefs(rs.sing)
	if err != nil {
		return result, err
	}
	spec := rs.querySpec().WithCustomFields(defs)
	for _, f := range spec.Fields {
		result.Controls.Columns = append(result.Controls.Columns, f.Label)
	}
	if controls {
		rows, err := tx.QueryContext(req.Context(), "SELECT DISTINCT name FROM tags WHERE entity_type=? ORDER BY name LIMIT 101", rs.sing)
		if err != nil {
			return result, err
		}
		for rows.Next() {
			var tag string
			if err := rows.Scan(&tag); err != nil {
				rows.Close()
				return result, err
			}
			result.Controls.Tags = append(result.Controls.Tags, tag)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return result, err
		}
		if len(result.Controls.Tags) > 100 {
			result.Controls.Tags = result.Controls.Tags[:100]
			result.Controls.TagSuggestionsLimited = true
		}
	}
	ids, total, err := store.InventoryIDs(req.Context(), tx, spec, store.InventoryFilter{Query: ctrl.Query, Tag: ctrl.Tag, Field: ctrl.FilterField, Value: ctrl.FilterValue, Sort: ctrl.Sort, Desc: ctrl.Dir == "desc"}, p.PerPage, p.offset())
	if err != nil {
		return result, err
	}
	for _, id := range ids {
		if err := req.Context().Err(); err != nil {
			return result, err
		}
		item, err := rs.repo.GetTx(tx, id)
		if err != nil {
			return result, err
		}
		result.Items = append(result.Items, item)
	}
	result.Values, err = cf.ValuesForEntities(rs.sing, ids)
	if err != nil {
		return result, err
	}
	if err := tx.Commit(); err != nil {
		return result, err
	}
	p.finish(req, total)
	result.Controls.Pagination = p
	return result, nil
}

func searchPage(req *http.Request, db *sql.DB, cat entityCatalog, p pagination) ([]searchEntry, pagination, error) {
	tx, err := db.BeginTx(req.Context(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, p, err
	}
	defer tx.Rollback()
	specs := make([]store.InventoryQuery, 0, len(cat.resources))
	for _, rs := range cat.resources {
		specs = append(specs, rs.querySpec())
	}
	hits, total, err := store.SearchInventory(req.Context(), tx, specs, strings.TrimSpace(req.URL.Query().Get("q")), p.PerPage, p.offset())
	if err != nil {
		return nil, p, err
	}
	entries := make([]searchEntry, 0, len(hits))
	for _, hit := range hits {
		entries = append(entries, searchEntry{Type: hit.Type, ID: hit.ID, Label: hit.Label, Path: cat.path(hit.Type, hit.ID)})
	}
	if err := tx.Commit(); err != nil {
		return nil, p, err
	}
	p.finish(req, total)
	return entries, p, nil
}
