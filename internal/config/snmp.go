package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Dealisto/almanaut/internal/snmp"
)

type snmpSourceFile struct {
	ID                  string `json:"id"`
	Name                string `json:"name"`
	Target              string `json:"target"`
	Port                uint16 `json:"port"`
	Version             string `json:"version"`
	Username            string `json:"username"`
	ContextName         string `json:"context_name"`
	AuthPasswordFile    string `json:"auth_password_file"`
	PrivacyPasswordFile string `json:"privacy_password_file"`
	CommunityFile       string `json:"community_file"`
	Interval            string `json:"interval"`
	OwnerType           string `json:"owner_type"`
	OwnerID             int64  `json:"owner_id"`
}

// LoadSNMPSources reads opt-in targets and file-based secrets. It returns no
// sources for an empty path and never discovers arbitrary subnets implicitly.
func LoadSNMPSources(path string) ([]snmp.Source, error) {
	if path == "" {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("cannot open SNMP source file")
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil || len(b) > 1<<20 {
		return nil, fmt.Errorf("SNMP source file exceeds one MiB or cannot be read")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	var records []snmpSourceFile
	if err := d.Decode(&records); err != nil || records == nil || len(records) > 32 {
		return nil, fmt.Errorf("SNMP source file must contain an array of at most 32 supported source records")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("SNMP source file must contain exactly one JSON document")
	}
	seen := map[string]bool{}
	sources := make([]snmp.Source, 0, len(records))
	for index, r := range records {
		s := snmp.Source{ID: r.ID, Name: r.Name, Target: r.Target, Port: r.Port, Version: r.Version, Username: r.Username, ContextName: r.ContextName, OwnerType: r.OwnerType, OwnerID: r.OwnerID}
		if s.Name == "" {
			s.Name = s.ID
		}
		if s.Version == "" {
			s.Version = "3"
		}
		if s.Port == 0 {
			s.Port = 161
		}
		if r.Interval != "" {
			s.Interval, err = time.ParseDuration(r.Interval)
			if err != nil {
				return nil, fmt.Errorf("SNMP source %d has an invalid interval", index+1)
			}
		}
		for _, secret := range []struct {
			file string
			out  *string
		}{{r.AuthPasswordFile, &s.AuthPassword}, {r.PrivacyPasswordFile, &s.PrivacyPassword}, {r.CommunityFile, &s.Community}} {
			if secret.file == "" {
				continue
			}
			filename := secret.file
			if !filepath.IsAbs(filename) {
				filename = filepath.Join(filepath.Dir(path), filename)
			}
			value, err := readSNMPSecret(filename)
			if err != nil {
				return nil, fmt.Errorf("SNMP source %d cannot read a valid credential file", index+1)
			}
			*secret.out = value
		}
		if err := s.Validate(); err != nil {
			return nil, fmt.Errorf("SNMP source %d: %w", index+1, err)
		}
		if seen[s.ID] {
			return nil, fmt.Errorf("duplicate SNMP source id")
		}
		seen[s.ID] = true
		sources = append(sources, s)
	}
	return sources, nil
}

func readSNMPSecret(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 1027))
	if err != nil {
		return "", err
	}
	v := strings.TrimRight(string(b), "\r\n")
	if len(b) > 1026 || v == "" || len(v) > 1024 || strings.ContainsAny(v, "\x00\r\n") {
		return "", fmt.Errorf("invalid credential")
	}
	return v, nil
}
