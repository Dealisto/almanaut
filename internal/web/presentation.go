package web

import (
	"html/template"
	"net/http"
	"strings"
	"time"
	"unicode"
)

// uiIcon only emits fixed application artwork; user input never becomes SVG.
func uiIcon(name string) template.HTML {
	paths := map[string]string{
		"grid":     `<rect x="3" y="3" width="7" height="7" rx="1.5"/><rect x="14" y="3" width="7" height="7" rx="1.5"/><rect x="3" y="14" width="7" height="7" rx="1.5"/><rect x="14" y="14" width="7" height="7" rx="1.5"/>`,
		"server":   `<rect x="3" y="3" width="18" height="7" rx="2"/><rect x="3" y="14" width="18" height="7" rx="2"/><path d="M7 6.5h.01M7 17.5h.01M12 6.5h5M12 17.5h5"/>`,
		"layers":   `<path d="m12 3 10 5-10 5L2 8l10-5Zm-10 9 10 5 10-5M2 16l10 5 10-5"/>`,
		"network":  `<rect x="8" y="2" width="8" height="6" rx="1"/><rect x="2" y="16" width="7" height="6" rx="1"/><rect x="15" y="16" width="7" height="6" rx="1"/><path d="M12 8v4M5.5 16v-4h13v4"/>`,
		"pulse":    `<path d="M2 12h4l3-8 6 16 3-8h4"/>`,
		"radar":    `<circle cx="12" cy="12" r="9"/><path d="m12 12 7-7M12 7a5 5 0 1 0 5 5"/><circle cx="12" cy="12" r="1"/>`,
		"history":  `<path d="M3 11a9 9 0 1 1 2.6 7M3 4v7h7M12 7v5l3 2"/>`,
		"home":     `<path d="m3 10 9-7 9 7v10a1 1 0 0 1-1 1h-5v-7H9v7H4a1 1 0 0 1-1-1V10Z"/>`,
		"search":   `<circle cx="10.5" cy="10.5" r="6.5"/><path d="m16 16 5 5"/>`,
		"sun":      `<circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M2 12h2M20 12h2M5 5l1 1M18 18l1 1M5 19l1-1M18 6l1-1"/>`,
		"moon":     `<path d="M21 13a9 9 0 1 1-10-10 7 7 0 0 0 10 10Z"/>`,
		"menu":     `<path d="M4 6h16M4 12h16M4 18h16"/>`,
		"plus":     `<path d="M12 5v14M5 12h14"/>`,
		"arrow":    `<path d="M4 12h16m-6-6 6 6-6 6"/>`,
		"chevron":  `<path d="m9 5 7 7-7 7"/>`,
		"close":    `<path d="m6 6 12 12M6 18 18 6"/>`,
		"shield":   `<path d="m12 3 8 3v6c0 5-8 9-8 9s-8-4-8-9V6l8-3Z"/><path d="m8 12 3 3 5-6"/>`,
		"alert":    `<path d="m10 4-8 14a2 2 0 0 0 2 3h16a2 2 0 0 0 2-3L14 4a2 2 0 0 0-4 0ZM12 9v4M12 17h.01"/>`,
		"box":      `<path d="m12 3 9 5v9l-9 5-9-5V8l9-5ZM3 8l9 5 9-5M12 13v9M7 5.5l10 5.5"/>`,
		"cable":    `<path d="M6 3v4m4-4v4M4 7h8v3a4 4 0 0 1-8 0V7ZM8 14v3a4 4 0 0 0 8 0v-3m-2-5h6v3a3 3 0 0 1-6 0V9ZM16 5v4m2-4v4"/>`,
		"check":    `<path d="m5 12 4 4L19 6"/>`,
		"settings": `<path d="M4 7h16M4 17h16"/><circle cx="8" cy="7" r="3"/><circle cx="16" cy="17" r="3"/>`,
		"building": `<rect x="4" y="3" width="16" height="18" rx="2"/><path d="M8 7h2m4 0h2M8 11h2m4 0h2M10 21v-6h4v6"/>`,
	}
	path, ok := paths[name]
	if !ok {
		path = paths["box"]
	}
	return template.HTML(`<svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">` + path + `</svg>`)
}

func displayTime(value string) string {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return value
	}
	return parsed.UTC().Format("Jan 2, 2006, 15:04 UTC")
}

// Only an application-owned fragment may be appended to the canonical path.
func detailReturnPath(r *http.Request, path string) string {
	if r.FormValue("return_section") == "activity" {
		return path + "#activity"
	}
	return path
}

func initials(name string) string {
	words := strings.Fields(name)
	var result []rune
	for _, word := range words {
		for _, r := range word {
			result = append(result, unicode.ToUpper(r))
			break
		}
		if len(result) == 2 {
			break
		}
	}
	return string(result)
}
