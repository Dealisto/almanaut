package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Dealisto/almanaut/internal/agentapi"
)

// ErrConflict reports that the server has this agent id bound to a different
// machine — a cloned VM whose state file was copied. It is the only outcome a
// human must act on, so the caller maps it to its own exit code.
var ErrConflict = errors.New("agent id is bound to a different machine")

// ErrRejected reports that the server refused the report for a reason that
// retrying will not fix: a bad token, or a schema it does not speak.
var ErrRejected = errors.New("server rejected the report")

// SendResult is the server's answer to an accepted report.
type SendResult struct {
	HostID  int64    `json:"host_id"`
	Changed []string `json:"changed"`
}

// maxErrorBody caps how much of an error response is read into a message, so a
// misconfigured URL pointing at something that streams cannot exhaust memory.
const maxErrorBody = 4 << 10

// Send posts one report and interprets the response.
func Send(ctx context.Context, hc *http.Client, serverURL, token string, rep agentapi.Report) (SendResult, error) {
	body, err := json.Marshal(rep)
	if err != nil {
		return SendResult{}, fmt.Errorf("encode report: %w", err)
	}
	url := strings.TrimRight(serverURL, "/") + "/api/agent/report"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return SendResult{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := hc.Do(req)
	if err != nil {
		return SendResult{}, fmt.Errorf("post report: %w", err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusOK:
		var res SendResult
		if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
			return SendResult{}, fmt.Errorf("decode response: %w", err)
		}
		if res.HostID == 0 {
			return SendResult{}, fmt.Errorf("%w: server returned 200 but no host_id at %s", ErrRejected, url)
		}
		return res, nil
	case resp.StatusCode == http.StatusConflict:
		return SendResult{}, fmt.Errorf("%w: %s", ErrConflict, serverMessage(resp.Body))
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		return SendResult{}, fmt.Errorf("%w (%d): %s", ErrRejected, resp.StatusCode, serverMessage(resp.Body))
	default:
		// 5xx and anything unexpected is transient: the timer runs again in an
		// hour, and the next report carries fresh facts anyway.
		return SendResult{}, fmt.Errorf("server returned %d: %s", resp.StatusCode, serverMessage(resp.Body))
	}
}

// serverMessage extracts {"error": "..."} from a response body, falling back
// to the raw text so an unexpected proxy error is still legible.
func serverMessage(r io.Reader) string {
	defer io.Copy(io.Discard, r)
	raw, err := io.ReadAll(io.LimitReader(r, maxErrorBody))
	if err != nil || len(raw) == 0 {
		return "(no message)"
	}
	var payload struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(raw, &payload) == nil && payload.Error != "" {
		return payload.Error
	}
	return strings.TrimSpace(string(raw))
}
