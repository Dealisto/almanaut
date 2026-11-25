package web

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// parseInput checks scalar fields before the shared form/CSV parser can turn
// malformed values into zero or false and silently change existing data.
// The entity's YAML fields are also the CSV and form field names. Empty fields
// retain their optional-zero semantics; domain validation checks required and
// out-of-range values after decoding.
func (rs resource[T]) parseInput(get func(string) string, id int64) (T, error) {
	fields := reflect.TypeFor[T]()
	scalars := make(map[string]string)
	var validation error
	for i := 0; i < fields.NumField(); i++ {
		field := fields.Field(i)
		kind := field.Type.Kind()
		if kind != reflect.Bool && (kind < reflect.Int || kind > reflect.Int64) {
			continue
		}
		name := strings.SplitN(field.Tag.Get("yaml"), ",", 2)[0]
		if name == "" || name == "-" || name == "id" {
			continue
		}
		raw := strings.TrimSpace(get(name))
		scalars[name] = raw
		if kind == reflect.Bool {
			switch strings.ToLower(raw) {
			case "", "on", "off", "true", "false", "1", "0", "yes", "no", "y", "n":
			default:
				if validation == nil {
					validation = fmt.Errorf("%s must be a valid boolean", name)
				}
			}
		} else if raw != "" {
			if _, err := strconv.ParseInt(raw, 10, field.Type.Bits()); err != nil && validation == nil {
				validation = fmt.Errorf("%s must be a valid integer", name)
			}
		}
	}
	item := rs.parse(func(name string) string {
		if raw, ok := scalars[name]; ok {
			return raw
		}
		return get(name)
	}, id)
	return item, validation
}
