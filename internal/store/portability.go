package store

import (
	"database/sql"

	"github.com/Dealisto/almanaut/internal/domain"
)

// Snapshot is the full inventory in a form that round-trips through YAML.
type Snapshot struct {
	Version       int                   `yaml:"version"`
	Hosts         []domain.Host         `yaml:"hosts"`
	Services      []domain.Service      `yaml:"services"`
	Networks      []domain.Network      `yaml:"networks"`
	Domains       []domain.Domain       `yaml:"domains"`
	Certificates  []domain.Certificate  `yaml:"certificates"`
	Backups       []domain.Backup       `yaml:"backups"`
	Relationships []domain.Relationship `yaml:"relationships"`
	Tags          []domain.Tag          `yaml:"tags"`
}

// Export gathers the entire inventory into a Snapshot, reusing the existing
// per-repo List() read paths.
func Export(db *sql.DB) (Snapshot, error) {
	hosts, err := NewHostRepo(db).List()
	if err != nil {
		return Snapshot{}, err
	}
	services, err := NewServiceRepo(db).List()
	if err != nil {
		return Snapshot{}, err
	}
	networks, err := NewNetworkRepo(db).List()
	if err != nil {
		return Snapshot{}, err
	}
	domains, err := NewDomainRepo(db).List()
	if err != nil {
		return Snapshot{}, err
	}
	certificates, err := NewCertificateRepo(db).List()
	if err != nil {
		return Snapshot{}, err
	}
	backups, err := NewBackupRepo(db).List()
	if err != nil {
		return Snapshot{}, err
	}
	relationships, err := NewRelationshipRepo(db).List()
	if err != nil {
		return Snapshot{}, err
	}
	tags, err := NewTagRepo(db).List()
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{
		Version:       1,
		Hosts:         hosts,
		Services:      services,
		Networks:      networks,
		Domains:       domains,
		Certificates:  certificates,
		Backups:       backups,
		Relationships: relationships,
		Tags:          tags,
	}, nil
}
