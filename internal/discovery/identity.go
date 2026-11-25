package discovery

import "github.com/Dealisto/almanaut/internal/domain"

// Candidate is a resource proposed by one configured discovery source.
type Candidate struct {
	ID, ParentID string
	Host         domain.Host
	Service      domain.Service
	EntityType   string
}

// Match identifies a persistent binding or an unambiguous manual record.
// ExistingID with Bound=false can be adopted during a confirmed import.
type Match struct {
	Candidate
	ExistingID int64
	Bound      bool
	Ambiguous  bool
}

// MatchCandidates prefers source identity over names. Name matching only adopts
// unbound records, so identically named resources from different sources remain
// separate. Inventory fields are never updated by matching or adoption.
func MatchCandidates(source string, candidates []Candidate, hosts []domain.Host, services []domain.Service, bindings []domain.DiscoveryBinding) []Match {
	type entityKey struct {
		kind string
		id   int64
	}
	type localRecord struct {
		id  int64
		typ string
	}
	bound := make(map[entityKey]bool)
	boundHere := make(map[entityKey]bool)
	boundNames := make(map[string]bool)
	byExternal := make(map[string]domain.DiscoveryBinding)
	local := make(map[entityKey]bool)
	names := make(map[string][]localRecord)
	for _, b := range bindings {
		bound[entityKey{b.EntityType, b.EntityID}] = true
		if b.Source == source {
			boundHere[entityKey{b.EntityType, b.EntityID}] = true
			byExternal[b.ExternalID] = b
		}
	}
	for _, h := range hosts {
		k := entityKey{"host", h.ID}
		local[k] = true
		if boundHere[k] {
			boundNames["host:"+NormalizeName(h.Name)] = true
		}
		if !bound[k] {
			key := "host:" + NormalizeName(h.Name)
			names[key] = append(names[key], localRecord{h.ID, h.Type})
		}
	}
	for _, s := range services {
		k := entityKey{"service", s.ID}
		local[k] = true
		if boundHere[k] {
			boundNames["service:"+NormalizeName(s.Name)] = true
		}
		if !bound[k] {
			key := "service:" + NormalizeName(s.Name)
			names[key] = append(names[key], localRecord{s.ID, s.Kind})
		}
	}
	candidateName := func(c Candidate) string {
		if c.EntityType == "host" {
			return "host:" + NormalizeName(c.Host.Name)
		}
		return c.EntityType + ":" + NormalizeName(c.Service.Name)
	}
	nameCounts, idCounts := make(map[string]int), make(map[string]int)
	for _, c := range candidates {
		nameCounts[candidateName(c)]++
		idCounts[c.ID]++
	}
	out := make([]Match, 0, len(candidates))
	for _, c := range candidates {
		m := Match{Candidate: c}
		if c.ID == "" || idCounts[c.ID] > 1 || (c.EntityType != "host" && c.EntityType != "service") {
			m.Ambiguous = true
		} else if b, ok := byExternal[c.ID]; ok {
			if b.EntityType == c.EntityType && local[entityKey{b.EntityType, b.EntityID}] {
				m.ExistingID = b.EntityID
				m.Bound = true
			} else {
				m.Ambiguous = true
			}
		} else {
			key := candidateName(c)
			matches := names[key]
			if boundNames[key] || (nameCounts[key] > 1 && len(matches) > 0) || len(matches) > 1 {
				m.Ambiguous = true
			} else if len(matches) == 1 {
				typ := c.Service.Kind
				if c.EntityType == "host" {
					typ = c.Host.Type
				}
				if matches[0].typ == typ {
					m.ExistingID = matches[0].id
				} else {
					m.Ambiguous = true
				}
			}
		}
		out = append(out, m)
	}
	return out
}
