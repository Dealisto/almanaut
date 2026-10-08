// Package redact strips secrets from URLs before they reach logs or errors.
//
// Notification and webhook endpoints often carry their credential in the URL
// itself: a Discord webhook's token is its last path segment, an ntfy topic is
// its own password, and outbound webhooks routinely embed a token in the path
// or query. net/http's *url.Error prints the full request URL (Go removes only
// a userinfo password), so a DNS failure or timeout would otherwise write the
// secret to the log on every failed attempt.
package redact

import (
	"fmt"
	"net/url"
)

// URL returns raw reduced to scheme and host, e.g. "https://discord.com/…".
// Path, query, fragment and userinfo are dropped, since any of them can hold a
// credential. An unparseable raw yields a fixed placeholder, never raw itself.
func URL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "<redacted url>"
	}
	s := u.Scheme + "://" + u.Host
	if u.Path != "" && u.Path != "/" || u.RawQuery != "" || u.Fragment != "" {
		s += "/…"
	}
	return s
}

// Error returns err with its URL reduced to URL(…) when err is a *url.Error —
// what http.Client.Do and url.Parse return — so the message can be logged
// safely. Apply it to the error straight from the call, before wrapping it.
// The underlying cause stays wrapped, so errors.Is / errors.As (e.g. for
// context.DeadlineExceeded) still work. Any other error is returned unchanged.
func Error(err error) error {
	ue, ok := err.(*url.Error)
	if !ok {
		return err
	}
	return &redactedError{op: ue.Op, url: URL(ue.URL), err: ue.Err}
}

type redactedError struct {
	op, url string
	err     error
}

func (e *redactedError) Error() string { return fmt.Sprintf("%s %q: %v", e.op, e.url, e.err) }
func (e *redactedError) Unwrap() error { return e.err }

// Timeout mirrors *url.Error, so a caller that classifies errors by it sees
// the same answer after redaction.
func (e *redactedError) Timeout() bool {
	t, ok := e.err.(interface{ Timeout() bool })
	return ok && t.Timeout()
}
