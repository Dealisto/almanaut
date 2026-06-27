package domain

import (
	"fmt"
	"strings"
)

// ServiceKinds is the closed set of allowed Service.Kind values.
var ServiceKinds = []string{"container", "native", "vm"}

// Service is an application or process running in the homelab.
type Service struct {
	ID       int64  `yaml:"id"`
	Name     string `yaml:"name"`
	Kind     string `yaml:"kind"` // container | native | vm
	URL      string `yaml:"url"` // access URL
	Ports    string `yaml:"ports"` // free text, e.g. "8096, 443"
	Category string `yaml:"category"`
	Notes    string `yaml:"notes"`
}

// Validate checks required fields and value formats.
func (s Service) Validate() error {
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if !contains(ServiceKinds, s.Kind) {
		return fmt.Errorf("kind %q must be one of %v", s.Kind, ServiceKinds)
	}
	return nil
}
