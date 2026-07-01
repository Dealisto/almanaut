package domain

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// validateOptionalDate returns an error when value is present but is not a valid
// DateLayout (YYYY-MM-DD) date; an empty value is allowed. field names the field
// in the error message. Shared by every entity with an optional date so they all
// reject the same inputs with the same wording.
func validateOptionalDate(field, value string) error {
	if v := strings.TrimSpace(value); v != "" {
		if _, err := time.Parse(DateLayout, v); err != nil {
			return fmt.Errorf("%s must be YYYY-MM-DD, got %q", field, value)
		}
	}
	return nil
}

// validateRequiredDate is validateOptionalDate plus a non-empty check.
func validateRequiredDate(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", field)
	}
	return validateOptionalDate(field, value)
}

// expiringOnOrBefore returns the items whose date (extracted by dateOf, in
// DateLayout) is on or before now+withinDays — including already-past dates —
// sorted by that date ascending. Items with an empty or unparseable date are
// skipped. It is the shared core of the certificate/warranty/renewal checks so
// they apply one identical cutoff and ordering rule.
func expiringOnOrBefore[T any](items []T, now time.Time, withinDays int, dateOf func(T) string) []T {
	cutoff := now.AddDate(0, 0, withinDays)
	out := []T{}
	for _, it := range items {
		d, err := time.Parse(DateLayout, dateOf(it))
		if err != nil {
			continue
		}
		if !d.After(cutoff) { // d <= cutoff
			out = append(out, it)
		}
	}
	// Dates are YYYY-MM-DD, which sorts lexically in chronological order.
	sort.Slice(out, func(i, j int) bool { return dateOf(out[i]) < dateOf(out[j]) })
	return out
}
