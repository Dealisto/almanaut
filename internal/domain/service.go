package domain

import (
	"fmt"
	"strings"
)

// ServiceKinds is the closed set of allowed Service.Kind values.
var ServiceKinds = []string{"container", "native", "vm"}

// Service is an application or process running in the homelab.
type Service struct {
	ID       int64
	Name     string
	Kind     string // container | native | vm
	URL      string // access URL
	Ports    string // free text, e.g. "8096, 443"
	Category string
	Notes    string
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
