package web

import (
	"fmt"
	"regexp"
	"strconv"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestUnassignedEquipmentEditAllowsStoredZeroHeight(t *testing.T) {
	srv, db := newTestServerDB(t)
	// API, agent and imported records can omit rack placement entirely.
	hostID, err := store.NewHostRepo(db).Create(domain.Host{Name: "agent-host", Type: "physical"})
	if err != nil {
		t.Fatal(err)
	}
	hardwareID, err := store.NewHardwareRepo(db).Create(domain.Hardware{Name: "imported-switch"})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{fmt.Sprintf("/hosts/%d/edit", hostID), fmt.Sprintf("/hardware/%d/edit", hardwareID)} {
		t.Run(path, func(t *testing.T) {
			body := getBody(t, srv, path)
			input := regexp.MustCompile(`<input\b[^>]*name="u_height"[^>]*>`).FindString(body)
			attributes := map[string]int{}
			for _, match := range regexp.MustCompile(`(min|max|value)="(-?\d+)"`).FindAllStringSubmatch(input, -1) {
				attributes[match[1]], err = strconv.Atoi(match[2])
				if err != nil {
					t.Fatal(err)
				}
			}
			height, ok := attributes["value"]
			if !ok || height != 0 {
				t.Fatalf("edit form changed the stored height: %s", input)
			}
			if min, ok := attributes["min"]; ok && height < min {
				t.Fatalf("browser would reject the stored height before submitting unrelated edits: %s", input)
			}
			if max, ok := attributes["max"]; ok && height > max {
				t.Fatalf("browser would reject the stored height before submitting unrelated edits: %s", input)
			}
		})
	}
}
