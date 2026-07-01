package domain

import (
	"fmt"
	"regexp"
	"strings"
)

// decimalAmount matches a non-negative decimal money string ("0", "12", "12.99").
// Using a regexp rather than strconv.ParseFloat deliberately rejects what the
// "decimal string, not a float" contract forbids: scientific notation ("1e3"),
// the float specials "Inf"/"NaN" (which slip past a v<0 check), and signs.
var decimalAmount = regexp.MustCompile(`^\d+(\.\d+)?$`)

// Subscription is a recurring cost or license: VPS, domain registration,
// software license, support contract. A perpetual license is a Subscription
// with BillingCycle "one-time". Amount is a validated decimal string, not a
// float, to avoid money rounding and match the all-text column convention.
type Subscription struct {
	ID           int64  `yaml:"id" json:"id"`
	Name         string `yaml:"name" json:"name"`
	Kind         string `yaml:"kind" json:"kind"`                   // free text: vps, domain, software license, ssl…
	Provider     string `yaml:"provider" json:"provider"`           // free text: Hetzner, Namecheap…
	Amount       string `yaml:"amount" json:"amount"`               // optional decimal string, e.g. "12.99"
	Currency     string `yaml:"currency" json:"currency"`           // free text short code: CAD, USD…
	BillingCycle string `yaml:"billing_cycle" json:"billing_cycle"` // free text: monthly, yearly, one-time
	RenewalDate  string `yaml:"renewal_date" json:"renewal_date"`   // optional YYYY-MM-DD
	AutoRenew    bool   `yaml:"auto_renew" json:"auto_renew"`
	Status       string `yaml:"status" json:"status"` // free text: active, cancelled…
	Notes        string `yaml:"notes" json:"notes"`
}

// Validate checks the name, the amount (a non-negative decimal if present),
// and the renewal date (YYYY-MM-DD if present).
func (s Subscription) Validate() error {
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if amt := strings.TrimSpace(s.Amount); amt != "" {
		if !decimalAmount.MatchString(amt) {
			return fmt.Errorf("amount must be a non-negative decimal, got %q", s.Amount)
		}
	}
	return validateOptionalDate("renewal date", s.RenewalDate)
}
