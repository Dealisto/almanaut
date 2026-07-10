package domain

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

// LivenessStatus is the most recent liveness check result for one entity. It is
// derived runtime state (never user-edited) and is attached to Host/Service for
// display via a repo-side join; nil means "no check has run yet".
type LivenessStatus struct {
	Status    string    // "up" | "down"
	CheckedAt time.Time // when the last probe ran
	ChangedAt time.Time // when Status last changed
	LastError string    // probe failure detail; "" when up
}

// Liveness status constants.
const (
	LivenessUp   = "up"
	LivenessDown = "down"
)

// ValidateCheckAddress accepts an empty address (entity not monitored) or a
// well-formed host:port with a numeric port in 1..65535.
func ValidateCheckAddress(addr string) error {
	if strings.TrimSpace(addr) == "" {
		return nil
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("check address %q must be host:port: %w", addr, err)
	}
	if strings.TrimSpace(host) == "" {
		return fmt.Errorf("check address %q has an empty host", addr)
	}
	p, err := strconv.Atoi(port)
	if err != nil || p < 1 || p > 65535 {
		return fmt.Errorf("check address %q has an invalid port", addr)
	}
	return nil
}
