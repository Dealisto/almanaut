package domain

import (
	"fmt"
	"strings"
)

// Account references an account or access associated with the homelab —
// admin logins, service accounts, API access, SSH keys. It never stores a
// secret: SecretRef points to where the credential lives (a password
// manager entry), not the credential itself.
type Account struct {
	ID              int64  `yaml:"id" json:"id"`
	Name            string `yaml:"name" json:"name"`
	Kind            string `yaml:"kind" json:"kind"`
	Username        string `yaml:"username" json:"username"`
	PasswordManager string `yaml:"password_manager" json:"password_manager"`
	SecretRef       string `yaml:"secret_ref" json:"secret_ref"`
	URL             string `yaml:"url" json:"url"`
	Status          string `yaml:"status" json:"status"`
	Notes           string `yaml:"notes" json:"notes"`
}

// Validate checks that the name is present. All other fields are optional
// free text. No secret is ever stored on an Account.
func (a Account) Validate() error {
	if strings.TrimSpace(a.Name) == "" {
		return fmt.Errorf("name is required")
	}
	return nil
}
