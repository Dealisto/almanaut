package web

import (
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
)

// dateInputType keeps the native picker for canonical dates. Browsers erase
// other representations, including padded dates accepted by the API, year zero
// and invalid drafts that must remain editable after server-side validation.
func dateInputType(value string) string {
	if value == "" {
		return "date"
	}
	date, err := time.Parse(domain.DateLayout, value)
	if err == nil && date.Year() > 0 && date.Format(domain.DateLayout) == value {
		return "date"
	}
	return "text"
}
