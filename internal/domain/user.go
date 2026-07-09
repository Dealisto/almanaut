package domain

import (
	"fmt"
	"strings"
)

// User is an application login account. It is distinct from the Account CMDB
// entity (which records where inventory credentials live). PasswordHash holds a
// bcrypt hash and is never serialised to JSON, so it cannot leak through the API.
type User struct {
	ID           int64  `json:"id"`
	Username     string `json:"username"`
	Role         Role   `json:"role"`
	PasswordHash string `json:"-"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

// MinPasswordLen is the shortest password the app accepts.
const MinPasswordLen = 8

// Validate checks that the username is present and the role is one of the
// built-in roles. The password is validated separately (it is not a field on
// User once hashed).
func (u User) Validate() error {
	if strings.TrimSpace(u.Username) == "" {
		return fmt.Errorf("username is required")
	}
	if !u.Role.Valid() {
		return fmt.Errorf("invalid role %q", u.Role)
	}
	return nil
}

// ValidatePassword enforces the minimum password length.
func ValidatePassword(pw string) error {
	if len(pw) < MinPasswordLen {
		return fmt.Errorf("password must be at least %d characters", MinPasswordLen)
	}
	return nil
}
