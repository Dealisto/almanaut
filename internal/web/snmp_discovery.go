package web

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/snmp"
	"github.com/Dealisto/almanaut/internal/store"
	"github.com/Dealisto/almanaut/internal/webhook"
	"github.com/go-chi/chi/v5"
)

// SNMPSource contains display metadata and a scanner, never credentials.
type SNMPSource struct {
	ID, Name, OwnerType, Fingerprint string
	OwnerID                          int64
	Scanner                          snmp.Scanner
}
type snmpInterfaceRow struct {
	snmp.Interface
	Expected   string
	Importable bool
}
type snmpNeighborRow struct {
	snmp.Neighbor
	Expected string
}
type snmpSourcePage struct {
	Title                          string
	Source                         SNMPSource
	Snapshot                       snmp.Snapshot
	Owner, OwnerURL, OwnerExpected string
	Interfaces                     []snmpInterfaceRow
	Neighbors                      []snmpNeighborRow
}
type snmpSourceStatus struct {
	Source   SNMPSource
	Snapshot snmp.Snapshot
}
type snmpPortOption struct {
	ID    int64
	Label string
}
type snmpLinkPage struct {
	Title                                  string
	Source                                 SNMPSource
	Neighbor                               snmp.Neighbor
	Expected, Query                        string
	Local, Remote                          []snmpPortOption
	Confirm                                bool
	LocalID, RemoteID                      int64
	LocalLabel, RemoteLabel, PortsExpected string
}

var errSNMPReview = errors.New("the observation or selected inventory changed; reload and review again")

func mountSNMPDiscovery(r chi.Router, sources []SNMPSource, db *sql.DB, cat entityCatalog, ports resource[domain.Port], d handlerDeps) {
	find := func(w http.ResponseWriter, req *http.Request) (SNMPSource, bool) {
		id := chi.URLParam(req, "source")
		for _, s := range sources {
			if s.ID == id {
				return s, true
			}
		}
		http.NotFound(w, req)
		return SNMPSource{}, false
	}
	r.Get("/discovery/snmp", func(w http.ResponseWriter, req *http.Request) {
		rows := []snmpSourceStatus{}
		for _, s := range sources {
			snap, err := snmp.Load(req.Context(), db, s.ID, s.Fingerprint)
			if err != nil {
				serverError(w, req, err)
				return
			}
			rows = append(rows, snmpSourceStatus{s, snap})
		}
		render(w, req, "snmp_sources.html", struct {
			Title   string
			Sources []snmpSourceStatus
		}{"SNMP discovery", rows})
	})
	r.Get("/discovery/snmp/{source}", func(w http.ResponseWriter, req *http.Request) {
		s, ok := find(w, req)
		if !ok {
			return
		}
		snap, err := snmp.Load(req.Context(), db, s.ID, s.Fingerprint)
		if err != nil {
			serverError(w, req, err)
			return
		}
		page := snmpSourcePage{Title: s.Name + " — SNMP", Source: s, Snapshot: snap}
		if s.OwnerID != 0 {
			page.Owner, err = snmpOwner(db, s)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				serverError(w, req, err)
				return
			}
			if err == nil {
				page.OwnerURL = cat.path(s.OwnerType, s.OwnerID)
				page.OwnerExpected = snmp.Fingerprint([]any{s.OwnerType, s.OwnerID, page.Owner})
			}
		}
		counts := map[string]int{}
		for _, i := range snap.Observation.Interfaces {
			counts[i.Name]++
		}
		for _, i := range snap.Observation.Interfaces {
			page.Interfaces = append(page.Interfaces, snmpInterfaceRow{i, snmpObservationToken(s, snap.Observation, i), i.PhysicalCandidate() && counts[i.Name] == 1 && page.Owner != ""})
		}
		for _, n := range snap.Observation.Neighbors {
			page.Neighbors = append(page.Neighbors, snmpNeighborRow{n, snmpObservationToken(s, snap.Observation, n)})
		}
		render(w, req, "snmp_source.html", page)
	})
	r.With(requireAdmin).Post("/discovery/snmp/{source}/scan", func(w http.ResponseWriter, req *http.Request) {
		s, ok := find(w, req)
		if !ok {
			return
		}
		if _, err := s.Scanner.Scan(req.Context()); err != nil {
			http.Error(w, "SNMP scan failed. The previous successful observation is preserved; inspect source status.", http.StatusBadGateway)
			return
		}
		http.Redirect(w, req, "/discovery/snmp/"+s.ID, http.StatusSeeOther)
	})
	r.With(requireWrite).Post("/discovery/snmp/{source}/import", func(w http.ResponseWriter, req *http.Request) {
		s, ok := find(w, req)
		if !ok || !snmpForm(w, req) {
			return
		}
		selected := req.PostForm["interface"]
		if s.OwnerID == 0 || len(selected) == 0 || len(selected) > snmp.MaxInterfaces {
			http.Error(w, "Select interfaces and configure an explicit existing owner.", http.StatusBadRequest)
			return
		}
		observed, err := s.Scanner.Scan(req.Context())
		if err != nil {
			http.Error(w, "A fresh SNMP observation is required; no inventory was changed.", http.StatusBadGateway)
			return
		}
		var events []webhook.Event
		err = store.WithTxContext(req.Context(), db, func(tx *sql.Tx) error {
			owner, err := snmpOwner(tx, s)
			if err != nil {
				return err
			}
			if req.PostForm.Get("owner_expected") != snmp.Fingerprint([]any{s.OwnerType, s.OwnerID, owner}) {
				return errSNMPReview
			}
			names := map[string]int{}
			available := map[string]snmp.Interface{}
			for _, i := range observed.Interfaces {
				names[i.Name]++
				available[snmpObservationToken(s, observed, i)] = i
			}
			batch := d
			batch.webhooks = webhook.ForTransaction(tx, d.webhooks)
			seen := map[string]bool{}
			for _, token := range selected {
				i, ok := available[token]
				if !ok || !i.PhysicalCandidate() || names[i.Name] != 1 || seen[token] {
					return errSNMPReview
				}
				seen[token] = true
				var exists bool
				if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM ports WHERE owner_type=? AND owner_id=? AND name=?)`, s.OwnerType, s.OwnerID, i.Name).Scan(&exists); err != nil {
					return err
				}
				if exists {
					return errSNMPReview
				}
				p := domain.Port{OwnerType: s.OwnerType, OwnerID: s.OwnerID, Name: i.Name, MAC: i.MAC, Notes: fmt.Sprintf("Created after reviewing SNMP source %s, interface index %d. Interface indexes are observations, not persistent inventory bindings.", s.ID, i.Index)}
				if err := p.Validate(); err != nil {
					return err
				}
				if _, err := ports.createEntityTx(tx, batch, p, nil, actor(req), &events); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			snmpMutationError(w, req, err)
			return
		}
		d.webhooks.Dispatch(events...)
		http.Redirect(w, req, "/discovery/snmp/"+s.ID, http.StatusSeeOther)
	})
	r.Get("/discovery/snmp/{source}/link", func(w http.ResponseWriter, req *http.Request) {
		s, ok := find(w, req)
		if !ok {
			return
		}
		snap, err := snmp.Load(req.Context(), db, s.ID, s.Fingerprint)
		if err != nil {
			serverError(w, req, err)
			return
		}
		n, ok := snmpFindNeighbor(s, snap.Observation, req.URL.Query().Get("neighbor"))
		if !ok || s.OwnerID == 0 {
			http.Error(w, "A complete LLDP observation and configured existing owner are required.", http.StatusConflict)
			return
		}
		page := snmpLinkPage{Title: "Review observed LLDP connection", Source: s, Neighbor: n, Expected: req.URL.Query().Get("neighbor"), Query: strings.TrimSpace(req.URL.Query().Get("q"))}
		page.Local, err = snmpPortOptions(db, s.OwnerType, s.OwnerID, "")
		if err == nil {
			page.Remote, err = snmpPortOptions(db, "", 0, page.Query)
		}
		if err != nil {
			serverError(w, req, err)
			return
		}
		render(w, req, "snmp_link.html", page)
	})
	r.With(requireWrite).Post("/discovery/snmp/{source}/link/review", func(w http.ResponseWriter, req *http.Request) {
		s, ok := find(w, req)
		if !ok || !snmpForm(w, req) {
			return
		}
		snap, err := snmp.Load(req.Context(), db, s.ID, s.Fingerprint)
		if err != nil {
			serverError(w, req, err)
			return
		}
		n, ok := snmpFindNeighbor(s, snap.Observation, req.PostForm.Get("neighbor"))
		if !ok {
			snmpMutationError(w, req, errSNMPReview)
			return
		}
		a, b, err := snmpReadPair(db, s, req.PostForm.Get("local"), req.PostForm.Get("remote"))
		if err != nil {
			snmpMutationError(w, req, err)
			return
		}
		page := snmpLinkPage{Title: "Confirm physical cable", Source: s, Neighbor: n, Expected: req.PostForm.Get("neighbor"), Confirm: true, LocalID: a.ID, RemoteID: b.ID, LocalLabel: a.OwnerName + " / " + a.Name, RemoteLabel: b.OwnerName + " / " + b.Name, PortsExpected: snmpPairToken(a, b)}
		render(w, req, "snmp_link.html", page)
	})
	r.With(requireWrite).Post("/discovery/snmp/{source}/link/connect", func(w http.ResponseWriter, req *http.Request) {
		s, ok := find(w, req)
		if !ok || !snmpForm(w, req) {
			return
		}
		observed, err := s.Scanner.Scan(req.Context())
		if err != nil {
			http.Error(w, "A fresh complete LLDP observation is required; no cable was changed.", http.StatusBadGateway)
			return
		}
		if _, ok := snmpFindNeighbor(s, observed, req.PostForm.Get("neighbor")); !ok {
			snmpMutationError(w, req, errSNMPReview)
			return
		}
		var events []webhook.Event
		err = store.WithTxContext(req.Context(), db, func(tx *sql.Tx) error {
			a, b, err := snmpReadPair(tx, s, req.PostForm.Get("local"), req.PostForm.Get("remote"))
			if err != nil {
				return err
			}
			if snmpPairToken(a, b) != req.PostForm.Get("ports_expected") {
				return errSNMPReview
			}
			before := a
			a.PeerPortID = b.ID
			if err := ports.repo.UpdateTx(tx, a); err != nil {
				return err
			}
			afterB, err := ports.repo.GetTx(tx, b.ID)
			if err != nil {
				return err
			}
			batch := d
			batch.webhooks = webhook.ForTransaction(tx, d.webhooks)
			// Record both actual stored endpoint changes without changing custom fields.
			if err := ports.recordUpdateTx(tx, batch, before, a, actor(req), &events); err != nil {
				return err
			}
			return ports.recordUpdateTx(tx, batch, b, afterB, actor(req), &events)
		})
		if err != nil {
			snmpMutationError(w, req, err)
			return
		}
		d.webhooks.Dispatch(events...)
		http.Redirect(w, req, "/topology?ref="+s.OwnerType+":"+strconv.FormatInt(s.OwnerID, 10), http.StatusSeeOther)
	})
}

func snmpObservationToken(s SNMPSource, o snmp.Observation, value any) string {
	return snmp.Fingerprint([]any{s.ID, s.Fingerprint, s.OwnerType, s.OwnerID, o.SystemName, o.ObjectID, o.Serial, value})
}
func snmpFindNeighbor(s SNMPSource, o snmp.Observation, token string) (snmp.Neighbor, bool) {
	if o.LLDPComplete {
		for _, n := range o.Neighbors {
			if snmpObservationToken(s, o, n) == token {
				return n, true
			}
		}
	}
	return snmp.Neighbor{}, false
}
func snmpPairToken(a, b domain.Port) string {
	return snmp.Fingerprint([]any{a, b, a.OwnerName, b.OwnerName})
}
func snmpForm(w http.ResponseWriter, r *http.Request) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form.", http.StatusBadRequest)
		return false
	}
	return true
}
func snmpMutationError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, errSNMPReview) || errors.Is(err, store.ErrNotFound) {
		http.Error(w, errSNMPReview.Error(), http.StatusConflict)
		return
	}
	serverError(w, r, err)
}
func snmpOwner(db store.DBTX, s SNMPSource) (string, error) {
	table := "hosts"
	if s.OwnerType == "hardware" {
		table = "hardware"
	} else if s.OwnerType != "host" {
		return "", store.ErrNotFound
	}
	var name string
	err := db.QueryRow(`SELECT name FROM `+table+` WHERE id=?`, s.OwnerID).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", store.ErrNotFound
	}
	return name, err
}
func snmpReadPair(db store.DBTX, s SNMPSource, local, remote string) (domain.Port, domain.Port, error) {
	read := func(raw string) (domain.Port, error) {
		var p domain.Port
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id < 1 {
			return p, errSNMPReview
		}
		err = db.QueryRow(`SELECT id,owner_type,owner_id,nic_id,name,mac,mgmt_only,peer_port_id,notes FROM ports WHERE id=?`, id).Scan(&p.ID, &p.OwnerType, &p.OwnerID, &p.NICID, &p.Name, &p.MAC, &p.MgmtOnly, &p.PeerPortID, &p.Notes)
		if errors.Is(err, sql.ErrNoRows) {
			return p, errSNMPReview
		}
		if err != nil {
			return p, err
		}
		p.OwnerName, err = snmpOwner(db, SNMPSource{OwnerType: p.OwnerType, OwnerID: p.OwnerID})
		return p, err
	}
	a, err := read(local)
	if err != nil {
		return a, domain.Port{}, err
	}
	b, err := read(remote)
	if err != nil {
		return a, b, err
	}
	if a.ID == b.ID || a.PeerPortID != 0 || b.PeerPortID != 0 || a.OwnerType != s.OwnerType || a.OwnerID != s.OwnerID {
		return a, b, errSNMPReview
	}
	return a, b, nil
}
func snmpPortOptions(db *sql.DB, ownerType string, ownerID int64, q string) ([]snmpPortOption, error) {
	if len(q) > 160 {
		return nil, fmt.Errorf("port search exceeds 160 bytes")
	}
	limit := 100
	if ownerType != "" {
		limit = snmp.MaxInterfaces
	}
	rows, err := db.Query(`SELECT p.id, COALESCE(h.name,hw.name)||' / '||p.name FROM ports p LEFT JOIN hosts h ON p.owner_type='host' AND p.owner_id=h.id LEFT JOIN hardware hw ON p.owner_type='hardware' AND p.owner_id=hw.id WHERE p.peer_port_id=0 AND (h.id IS NOT NULL OR hw.id IS NOT NULL) AND (?='' OR (p.owner_type=? AND p.owner_id=?)) AND (?='' OR instr(lower(COALESCE(h.name,hw.name)||' / '||p.name),lower(?))>0) ORDER BY COALESCE(h.name,hw.name),p.name,p.id LIMIT ?`, ownerType, ownerType, ownerID, q, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []snmpPortOption{}
	for rows.Next() {
		var p snmpPortOption
		if err := rows.Scan(&p.ID, &p.Label); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
