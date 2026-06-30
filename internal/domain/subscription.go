package domain

import (
	"fmt"
	"regexp"
	"strings"
	"time"
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
	ID           int64  `yaml:"id"`
	Name         string `yaml:"name"`
	Kind         string `yaml:"kind"`          // free text: vps, domain, software license, ssl…
	Provider     string `yaml:"provider"`      // free text: Hetzner, Namecheap…
	Amount       string `yaml:"amount"`        // optional decimal string, e.g. "12.99"
	Currency     string `yaml:"currency"`      // free text short code: CAD, USD…
	BillingCycle string `yaml:"billing_cycle"` // free text: monthly, yearly, one-time
	RenewalDate  string `yaml:"renewal_date"`  // optional YYYY-MM-DD
	AutoRenew    bool   `yaml:"auto_renew"`
	Status       string `yaml:"status"` // free text: active, cancelled…
	Notes        string `yaml:"notes"`
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
	if rd := strings.TrimSpace(s.RenewalDate); rd != "" {
		if _, err := time.Parse(DateLayout, rd); err != nil {
			return fmt.Errorf("renewal date must be YYYY-MM-DD, got %q", s.RenewalDate)
		}
	}
	return nil
}
