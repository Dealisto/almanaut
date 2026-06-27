package web

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Dealisto/almanaut/internal/store"
)

// entityOption is one selectable entity in a relationship dropdown.
type entityOption struct {
	Value string // "type:id"
	Label string // "host: proxmox"
	Type  string
	ID    int64
}

// entityCatalog aggregates the six entity repositories so the relationship UI
// can list every entity and resolve (type,id) references to human labels.
type entityCatalog struct {
	hosts        *store.HostRepo
	services     *store.ServiceRepo
	networks     *store.NetworkRepo
	domains      *store.DomainRepo
	certificates *store.CertificateRepo
	backups      *store.BackupRepo
}

func entityOptionOf(typ string, id int64, name string) entityOption {
	return entityOption{
		Value: fmt.Sprintf("%s:%d", typ, id),
		Label: fmt.Sprintf("%s: %s", typ, name),
		Type:  typ,
		ID:    id,
	}
}

// options returns every entity across all six types as selectable options.
func (c entityCatalog) options() ([]entityOption, error) {
	var opts []entityOption

	hosts, err := c.hosts.List()
	if err != nil {
		return nil, err
	}
	for _, h := range hosts {
		opts = append(opts, entityOptionOf("host", h.ID, h.Name))
	}
	services, err := c.services.List()
	if err != nil {
		return nil, err
	}
	for _, s := range services {
		opts = append(opts, entityOptionOf("service", s.ID, s.Name))
	}
	networks, err := c.networks.List()
	if err != nil {
		return nil, err
	}
	for _, n := range networks {
		opts = append(opts, entityOptionOf("network", n.ID, n.Name))
	}
	domains, err := c.domains.List()
	if err != nil {
		return nil, err
	}
	for _, d := range domains {
		opts = append(opts, entityOptionOf("domain", d.ID, d.FQDN))
	}
	certs, err := c.certificates.List()
	if err != nil {
		return nil, err
	}
	for _, ct := range certs {
		opts = append(opts, entityOptionOf("certificate", ct.ID, ct.Subject))
	}
	backups, err := c.backups.List()
	if err != nil {
		return nil, err
	}
	for _, b := range backups {
		opts = append(opts, entityOptionOf("backup", b.ID, b.Source))
	}
	return opts, nil
}

// labelMap builds a lookup from each option's "type:id" value to its label,
// used to resolve relationship endpoints and tagged entities to human names.
func labelMap(opts []entityOption) map[string]string {
	labels := make(map[string]string, len(opts))
	for _, o := range opts {
		labels[o.Value] = o.Label
	}
	return labels
}

// parseRef splits a "type:id" reference string.
func parseRef(s string) (string, int64, error) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 || parts[0] == "" {
		return "", 0, fmt.Errorf("invalid entity reference %q", s)
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return "", 0, fmt.Errorf("invalid entity reference %q", s)
	}
	return parts[0], id, nil
}

// labelOrFallback resolves (typ,id) to a label, or a "(deleted)" placeholder
// when the entity no longer exists.
func labelOrFallback(labels map[string]string, typ string, id int64) string {
	key := fmt.Sprintf("%s:%d", typ, id)
	if l, ok := labels[key]; ok {
		return l
	}
	return key + " (deleted)"
}
