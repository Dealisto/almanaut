package domain

import "fmt"

// HealthRuleDefinition gives built-in checks stable settings identifiers.
// Rules contain no user-provided expressions or queries.
type HealthRuleDefinition struct {
	ID, Title      string
	DefaultEnabled bool
}

var HealthRuleDefinitions = []HealthRuleDefinition{
	{"hosts_without_backup", "Hosts without a backup", true},
	{"services_without_host", "Services not linked to a host", true},
	{"expired_certificates", "Expired certificates", true},
	{"unlinked_certificates", "Certificates linked to nothing", true},
	{"hardware_without_warranty", "Hardware without a warranty date", true},
	{"subscriptions_without_renewal", "Subscriptions without a renewal date", true},
	{"orphaned_entities", "Orphaned entities", true},
	{"duplicate_ips", "Duplicate IP assignments", true},
	{"ips_outside_networks", "IP allocations outside every network", true},
	{"overlapping_networks", "Overlapping networks", true},
	{"stale_entities", "Stale entities", true},
	{"certificates_due_soon", "Certificates expiring soon", false},
	{"backup-job-health", "Backup job health", true},
}

type HealthRuleSettings struct {
	Enabled                map[string]bool `json:"enabled"`
	StaleAfterDays         *int            `json:"stale_after_days"`
	CertificateWarningDays int             `json:"certificate_warning_days"`
}

func DefaultHealthRuleSettings() HealthRuleSettings {
	return HealthRuleSettings{Enabled: map[string]bool{}, CertificateWarningDays: 30}
}

func (s HealthRuleSettings) RuleEnabled(id string) bool {
	for _, rule := range HealthRuleDefinitions {
		if rule.ID == id {
			if enabled, ok := s.Enabled[id]; ok {
				return enabled
			}
			return rule.DefaultEnabled
		}
	}
	return false
}

func (s HealthRuleSettings) Validate() error {
	known := make(map[string]bool, len(HealthRuleDefinitions))
	for _, rule := range HealthRuleDefinitions {
		known[rule.ID] = true
	}
	for id := range s.Enabled {
		if !known[id] {
			return fmt.Errorf("unknown health rule %q", id)
		}
	}
	if s.StaleAfterDays != nil && (*s.StaleAfterDays < 0 || *s.StaleAfterDays > 36500) {
		return fmt.Errorf("staleness must be between 0 and 36500 days")
	}
	if s.CertificateWarningDays < 1 || s.CertificateWarningDays > 3650 {
		return fmt.Errorf("certificate warning must be between 1 and 3650 days")
	}
	return nil
}
