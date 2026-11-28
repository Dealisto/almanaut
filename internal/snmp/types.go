// Package snmp collects read-only device observations. It never writes to a
// network device or treats an observed identity as an inventory association.
package snmp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"
)

const MaxInterfaces = 512
const MaxNeighbors = 512
const ScanTimeout = 30 * time.Second

// Source is explicit operator configuration. Credentials are never serialized.
type Source struct {
	ID, Name, Target, Version, Username, ContextName string
	Port                                             uint16
	AuthPassword                                     string `json:"-" yaml:"-"`
	PrivacyPassword                                  string `json:"-" yaml:"-"`
	Community                                        string `json:"-" yaml:"-"`
	Interval                                         time.Duration
	OwnerType                                        string
	OwnerID                                          int64
}

var sourceIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

func (s Source) String() string   { return fmt.Sprintf("SNMPSource{ID:%q Version:%q}", s.ID, s.Version) }
func (s Source) GoString() string { return s.String() }

// Fingerprint binds observations to explicit nonsecret source configuration.
// Credential rotation does not change the identity of a configured endpoint.
func (s Source) Fingerprint() string {
	return Fingerprint([]any{s.ID, s.Target, s.Port, s.Version, s.Username, s.ContextName, s.OwnerType, s.OwnerID})
}

func (s Source) Validate() error {
	if !sourceIDPattern.MatchString(s.ID) {
		return fmt.Errorf("SNMP source id must contain 1-64 lowercase letters, digits, underscores or hyphens")
	}
	if len(s.Name) > 160 || strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("SNMP source name must contain 1-160 bytes")
	}
	if s.Target == "" || len(s.Target) > 253 || strings.ContainsAny(s.Target, " /\\\t\n\r?#@") || (strings.Contains(s.Target, ":") && net.ParseIP(s.Target) == nil) {
		return fmt.Errorf("SNMP target must be an IP address or hostname without a port")
	}
	if s.Port == 0 {
		return fmt.Errorf("SNMP port must be positive")
	}
	if s.Interval < 0 || (s.Interval > 0 && s.Interval < time.Minute) {
		return fmt.Errorf("SNMP interval must be zero or at least one minute")
	}
	if s.OwnerID < 0 || (s.OwnerID == 0) != (s.OwnerType == "") || (s.OwnerType != "" && s.OwnerType != "host" && s.OwnerType != "hardware") {
		return fmt.Errorf("SNMP owner must specify host or hardware and a positive owner_id together")
	}
	switch s.Version {
	case "3":
		if len(s.Username) < 1 || len(s.Username) > 32 || len(s.AuthPassword) < 8 || len(s.PrivacyPassword) < 8 || len(s.AuthPassword) > 1024 || len(s.PrivacyPassword) > 1024 || s.Community != "" {
			return fmt.Errorf("SNMPv3 requires a username and two 8-1024 byte passphrases, without a community")
		}
	case "2c":
		if s.Community == "" || len(s.Community) > 1024 || s.Username != "" || s.AuthPassword != "" || s.PrivacyPassword != "" || s.ContextName != "" {
			return fmt.Errorf("SNMPv2c requires only a community credential")
		}
	default:
		return fmt.Errorf("SNMP version must be 3 or explicitly 2c")
	}
	if len(s.ContextName) > 255 {
		return fmt.Errorf("SNMP context name is too long")
	}
	return nil
}

type Interface struct {
	Index       int    `json:"index"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Alias       string `json:"alias"`
	Type        int    `json:"type"`
	MAC         string `json:"mac"`
	SpeedMbps   uint64 `json:"speed_mbps"`
	AdminStatus int    `json:"admin_status"`
	OperStatus  int    `json:"oper_status"`
}

// PhysicalCandidate limits port creation to observed ethernet interfaces.
// ifType alone does not prove a physical connector: import still needs review.
func (i Interface) PhysicalCandidate() bool { return i.Type == 6 && i.Name != "" }

type Neighbor struct {
	LocalNumber      int    `json:"local_number"`
	LocalSubtype     int    `json:"local_subtype"`
	LocalID          string `json:"local_id"`
	LocalDescription string `json:"local_description"`
	ChassisSubtype   int    `json:"chassis_subtype"`
	ChassisID        string `json:"chassis_id"`
	PortSubtype      int    `json:"port_subtype"`
	PortID           string `json:"port_id"`
	PortDescription  string `json:"port_description"`
	SystemName       string `json:"system_name"`
}

type Observation struct {
	SystemName   string      `json:"system_name"`
	Description  string      `json:"description"`
	ObjectID     string      `json:"object_id"`
	Manufacturer string      `json:"manufacturer"`
	Model        string      `json:"model"`
	Serial       string      `json:"serial"`
	Interfaces   []Interface `json:"interfaces"`
	Neighbors    []Neighbor  `json:"neighbors"`
	LLDPComplete bool        `json:"lldp_complete"`
	Warnings     []string    `json:"warnings"`
}

type Scanner interface {
	Scan(context.Context) (Observation, error)
}

// Fingerprint detects stale reviews; it is never an authorization credential.
func Fingerprint(value any) string {
	b, _ := json.Marshal(value)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
