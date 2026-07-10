package domain

import (
	"fmt"
	"strings"
)

// ServiceKinds is the closed set of allowed Service.Kind values.
var ServiceKinds = []string{"container", "native", "vm"}

// Service is an application or process running in the homelab.
type Service struct {
	ID       int64  `yaml:"id" json:"id"`
	Name     string `yaml:"name" json:"name"`
	Kind     string `yaml:"kind" json:"kind"`   // container | native | vm
	URL      string `yaml:"url" json:"url"`     // access URL
	Ports    string `yaml:"ports" json:"ports"` // free text, e.g. "8096, 443"
	Category string `yaml:"category" json:"category"`

	CheckAddress string          `yaml:"check_address" json:"check_address"` // optional host:port for TCP liveness checks; empty = not monitored
	Liveness     *LivenessStatus `yaml:"-" json:"liveness,omitempty"`        // derived, populated by the repo join

	Notes string `yaml:"notes" json:"notes"`
}

// Validate checks required fields and value formats.
func (s Service) Validate() error {
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if !contains(ServiceKinds, s.Kind) {
		return fmt.Errorf("kind %q must be one of %v", s.Kind, ServiceKinds)
	}
	if err := ValidateCheckAddress(s.CheckAddress); err != nil {
		return err
	}
	return nil
}
