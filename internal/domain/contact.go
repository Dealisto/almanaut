package domain

import (
	"fmt"
	"strings"
)

// Contact is a person or vendor responsible for infrastructure (admin, owner,
// supplier). Link it to entities via relationships ("administered by", "owned by").
type Contact struct {
	ID           int64  `yaml:"id" json:"id"`
	Name         string `yaml:"name" json:"name"`
	Email        string `yaml:"email" json:"email"`
	Phone        string `yaml:"phone" json:"phone"`
	Role         string `yaml:"role" json:"role"`
	Organization string `yaml:"organization" json:"organization"`
	Notes        string `yaml:"notes" json:"notes"`
}

// Validate requires a name and, if an email is given, a minimal "@" check.
func (c Contact) Validate() error {
	if strings.TrimSpace(c.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if e := strings.TrimSpace(c.Email); e != "" && !strings.Contains(e, "@") {
		return fmt.Errorf("invalid email %q", c.Email)
	}
	return nil
}
