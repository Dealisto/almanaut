package discovery

import (
	"encoding/json"
	"fmt"
	"io"
)

// decodeInventory requires one complete JSON document within the response cap.
// A valid prefix followed by garbage or excess data is not a successful scan:
// callers use successful scans to decide whether previously seen items vanished.
func decodeInventory(r io.Reader, dst any) error {
	const maxBody = 16 << 20
	body, err := io.ReadAll(io.LimitReader(r, maxBody+1))
	if err != nil {
		return err
	}
	if len(body) > maxBody {
		return fmt.Errorf("inventory response exceeds 16 MiB")
	}
	return json.Unmarshal(body, dst)
}

// discoveryRequestError preserves the cause while exposing a sanitized message.
type discoveryRequestError struct {
	err     error
	message string
}

func (e *discoveryRequestError) Error() string { return e.message }
func (e *discoveryRequestError) Unwrap() error { return e.err }
