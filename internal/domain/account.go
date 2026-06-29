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
	ID              int64  `yaml:"id"`
	Name            string `yaml:"name"`
	Kind            string `yaml:"kind"`
	Username        string `yaml:"username"`
	PasswordManager string `yaml:"password_manager"`
	SecretRef       string `yaml:"secret_ref"`
	URL             string `yaml:"url"`
	Status          string `yaml:"status"`
	Notes           string `yaml:"notes"`
}

// Validate checks that the name is present. All other fields are optional
// free text. No secret is ever stored on an Account.
func (a Account) Validate() error {
	if strings.TrimSpace(a.Name) == "" {
		return fmt.Errorf("name is required")
	}
	return nil
}
