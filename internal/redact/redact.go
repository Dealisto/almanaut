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
	"strings"
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

// ErrorWithSecrets also filters credentials reflected in underlying protocol
// or body-read errors. URL redaction alone cannot hide a token in, for example,
// a malformed HTTP status line. Error identity and timeout behavior are kept.
func ErrorWithSecrets(err error, secrets ...string) error {
	if err == nil {
		return nil
	}
	pairs := make([]string, 0, len(secrets)*2)
	for _, secret := range secrets {
		if secret != "" {
			pairs = append(pairs, secret, "[redacted]")
		}
	}
	return &secretError{err: Error(err), replacer: strings.NewReplacer(pairs...)}
}

type secretError struct {
	err      error
	replacer *strings.Replacer
}

func (e *secretError) Error() string { return e.replacer.Replace(e.err.Error()) }
func (e *secretError) Unwrap() error { return e.err }
func (e *secretError) Timeout() bool {
	t, ok := e.err.(interface{ Timeout() bool })
	return ok && t.Timeout()
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
