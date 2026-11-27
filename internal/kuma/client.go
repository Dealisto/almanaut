package kuma

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/Dealisto/almanaut/internal/redact"
	"github.com/coder/websocket"
)

// Client dials Uptime Kuma's internal socket.io API. Kuma has no official API
// for monitor CRUD: the web UI's socket.io protocol is the only surface, so we
// speak a deliberately tiny subset (login + monitor add/edit/delete) and treat
// every response defensively.
type Client struct {
	wsURL    string
	user     string
	pass     string
	insecure bool
}

// NewClient prepares a client for the Kuma instance at baseURL
// (e.g. "http://kuma.lan:3001"). No connection is made until Connect.
func NewClient(baseURL, user, pass string, insecure bool) *Client {
	base := strings.TrimRight(baseURL, "/")
	ws := base
	switch {
	case strings.HasPrefix(base, "https://"):
		ws = "wss://" + strings.TrimPrefix(base, "https://")
	case strings.HasPrefix(base, "http://"):
		ws = "ws://" + strings.TrimPrefix(base, "http://")
	}
	return &Client{wsURL: ws + "/socket.io/?EIO=4&transport=websocket", user: user, pass: pass, insecure: insecure}
}

// Session is one authenticated socket.io connection with the initial
// monitorList captured. It is not safe for concurrent emits from multiple
// goroutines except through the internal mutex on write.
type Session struct {
	conn     *websocket.Conn
	cancel   context.CancelFunc // stops the read loop
	endpoint string
	secrets  []string // immutable credentials retained for post-login error redaction
	writeMu  sync.Mutex
	ackMu    sync.Mutex
	ackID    int64
	acks     map[int64]chan json.RawMessage
	monsOnce chan struct{} // closed when the first monitorList arrives
	monsMu   sync.Mutex
	monitors map[int64]Monitor
	readErr  chan error
}

// Connect dials, completes the engine.io v4 + socket.io handshake, logs in,
// and waits for the initial monitorList push. Close the session when done.
func (c *Client) Connect(ctx context.Context) (_ *Session, err error) {
	defer func() {
		err = redact.ErrorWithURL(err, c.wsURL, c.user, c.pass)
	}()
	// The login frame carries credentials after the HTTP upgrade. Following a
	// redirect would authenticate to a different endpoint, even though net/http
	// normally strips authorization headers on cross-host redirects.
	httpClient := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	if c.insecure {
		httpClient.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
	}
	conn, _, err := websocket.Dial(ctx, c.wsURL, &websocket.DialOptions{HTTPClient: httpClient})
	if err != nil {
		return nil, fmt.Errorf("kuma: dial: %w", err)
	}
	conn.SetReadLimit(16 << 20) // monitorList can be large; still bounded

	readFrame := func() (string, error) {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}

	// engine.io OPEN, then socket.io CONNECT on the default namespace.
	frame, err := readFrame()
	if err != nil {
		conn.CloseNow()
		return nil, fmt.Errorf("kuma: engine.io open: %w", err)
	}
	if !strings.HasPrefix(frame, "0") {
		conn.CloseNow()
		return nil, errors.New("kuma: invalid engine.io open frame")
	}
	if err := conn.Write(ctx, websocket.MessageText, []byte("40")); err != nil {
		conn.CloseNow()
		return nil, fmt.Errorf("kuma: socket.io connect: %w", err)
	}
	for {
		frame, err = readFrame()
		if err != nil {
			conn.CloseNow()
			return nil, fmt.Errorf("kuma: socket.io connect ack: %w", err)
		}
		if strings.HasPrefix(frame, "40") {
			break
		}
		if frame == "2" { // server ping during handshake
			if err := conn.Write(ctx, websocket.MessageText, []byte("3")); err != nil {
				conn.CloseNow()
				return nil, fmt.Errorf("kuma: pong: %w", err)
			}
		}
	}

	loopCtx, cancel := context.WithCancel(context.Background())
	s := &Session{
		conn: conn, cancel: cancel,
		endpoint: c.wsURL, secrets: []string{c.user, c.pass},
		acks:     map[int64]chan json.RawMessage{},
		monsOnce: make(chan struct{}),
		monitors: map[int64]Monitor{},
		readErr:  make(chan error, 1),
	}
	go s.readLoop(loopCtx)

	res, err := s.emit(ctx, "login", map[string]string{"username": c.user, "password": c.pass, "token": ""})
	if err != nil {
		s.Close()
		return nil, fmt.Errorf("kuma: login: %w", err)
	}
	if err := okOrError(res, c.wsURL, c.user, c.pass); err != nil {
		s.Close()
		return nil, fmt.Errorf("kuma: login: %w", err)
	}

	// Kuma pushes the monitor list right after a successful login.
	select {
	case <-s.monsOnce:
	case err := <-s.readErr:
		s.Close()
		return nil, fmt.Errorf("kuma: waiting for monitorList: %w", err)
	case <-ctx.Done():
		s.Close()
		return nil, fmt.Errorf("kuma: waiting for monitorList: %w", ctx.Err())
	}
	return s, nil
}

// safeError protects errors throughout the session, including transport errors
// returned after a successful login. The underlying cause remains available.
func (s *Session) safeError(err error) error {
	return redact.ErrorWithURL(err, s.endpoint, s.secrets...)
}

// Close tears the connection down. Safe to call more than once.
func (s *Session) Close() {
	s.cancel()
	s.conn.CloseNow()
}

// readLoop is the single reader: it answers pings, resolves acks, and captures
// monitorList pushes. It exits when the connection drops or Close is called.
func (s *Session) readLoop(ctx context.Context) {
	for {
		_, data, err := s.conn.Read(ctx)
		if err != nil {
			select {
			case s.readErr <- err:
			default:
			}
			s.failPendingAcks()
			return
		}
		msg := string(data)
		switch {
		case msg == "2": // engine.io ping → pong, or Kuma drops us
			s.write(ctx, "3")
		case strings.HasPrefix(msg, "43"): // ack
			s.resolveAck(msg)
		case strings.HasPrefix(msg, "42"): // server-initiated event
			if err := s.handleEvent(msg); err != nil {
				select {
				case s.readErr <- err:
				default:
				}
				s.conn.CloseNow()
				s.failPendingAcks()
				return
			}
		}
	}
}

func (s *Session) write(ctx context.Context, frame string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.conn.Write(ctx, websocket.MessageText, []byte(frame))
}

// emit sends `42<id>["event",arg]` and waits for the matching `43<id>[...]`.
func (s *Session) emit(ctx context.Context, event string, arg any) (json.RawMessage, error) {
	payload, err := json.Marshal([]any{event, arg})
	if err != nil {
		return nil, err
	}
	s.ackMu.Lock()
	s.ackID++
	id := s.ackID
	ch := make(chan json.RawMessage, 1)
	s.acks[id] = ch
	s.ackMu.Unlock()
	defer func() {
		s.ackMu.Lock()
		delete(s.acks, id)
		s.ackMu.Unlock()
	}()

	if err := s.write(ctx, "42"+strconv.FormatInt(id, 10)+string(payload)); err != nil {
		return nil, err
	}
	select {
	case res, ok := <-ch:
		if !ok {
			return nil, errors.New("connection closed")
		}
		return res, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// resolveAck parses "43<id>[result...]" and delivers the first array element.
func (s *Session) resolveAck(msg string) {
	rest := msg[2:]
	i := 0
	for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
		i++
	}
	id, err := strconv.ParseInt(rest[:i], 10, 64)
	if err != nil {
		return
	}
	var arr []json.RawMessage
	if err := json.Unmarshal([]byte(rest[i:]), &arr); err != nil || len(arr) == 0 {
		return
	}
	s.ackMu.Lock()
	ch := s.acks[id]
	s.ackMu.Unlock()
	if ch != nil {
		select {
		case ch <- arr[0]:
		default: // duplicate ack for an id whose buffer is full — drop it
		}
	}
}

// failPendingAcks closes every pending ack channel so emit callers waiting on
// them unblock instead of hanging forever after the connection drops.
func (s *Session) failPendingAcks() {
	s.ackMu.Lock()
	defer s.ackMu.Unlock()
	for id, ch := range s.acks {
		close(ch)
		delete(s.acks, id)
	}
}

// handleEvent captures server pushes we care about (monitorList); the rest of
// Kuma's chatty updates (heartbeats, uptime, ...) are ignored.
func (s *Session) handleEvent(msg string) error {
	rest := strings.TrimLeft(msg[2:], "0123456789")
	var arr []json.RawMessage
	if err := json.Unmarshal([]byte(rest), &arr); err != nil || len(arr) < 2 {
		return nil
	}
	var event string
	if err := json.Unmarshal(arr[0], &event); err != nil || event != "monitorList" {
		return nil
	}
	var list map[string]map[string]any
	// Some socket.io versions encode an empty collection as []. Only an
	// empty array is equivalent to an empty object; null and malformed lists
	// must not erase our view of existing monitors and trigger replacements.
	if strings.HasPrefix(strings.TrimSpace(string(arr[1])), "[") {
		var entries []json.RawMessage
		if err := json.Unmarshal(arr[1], &entries); err != nil || len(entries) != 0 {
			return errors.New("kuma: invalid monitorList array")
		}
		list = map[string]map[string]any{}
	} else {
		// These objects are sent back on edit. Preserve unknown numeric
		// settings exactly instead of rounding them through float64.
		decoder := json.NewDecoder(bytes.NewReader(arr[1]))
		decoder.UseNumber()
		if err := decoder.Decode(&list); err != nil || list == nil {
			return errors.New("kuma: invalid monitorList object")
		}
	}
	mons := make(map[int64]Monitor, len(list))
	for key, raw := range list {
		id, err := strconv.ParseInt(key, 10, 64)
		_, duplicate := mons[id]
		if err != nil || id <= 0 || raw == nil || duplicate {
			return errors.New("kuma: invalid monitorList entry")
		}
		m := Monitor{ID: id, raw: raw}
		name, ok := raw["name"].(string)
		if !ok {
			return errors.New("kuma: invalid monitorList name")
		}
		m.Name = name
		if value, exists := raw["url"]; exists && value != nil {
			url, ok := value.(string)
			if !ok {
				return errors.New("kuma: invalid monitorList URL")
			}
			m.URL = url
		}
		mons[id] = m
	}
	s.monsMu.Lock()
	s.monitors = mons
	s.monsMu.Unlock()
	select {
	case <-s.monsOnce: // already signalled
	default:
		close(s.monsOnce)
	}
	return nil
}

// Monitors returns the most recent monitor list pushed by the server.
func (s *Session) Monitors() map[int64]Monitor {
	s.monsMu.Lock()
	defer s.monsMu.Unlock()
	out := make(map[int64]Monitor, len(s.monitors))
	for id, m := range s.monitors {
		out[id] = m
	}
	return out
}

// ackResult is the common shape of Kuma's operation acks.
type ackResult struct {
	OK        bool   `json:"ok"`
	Msg       string `json:"msg"`
	MonitorID int64  `json:"monitorID"`
}

// okOrError decodes an ack and converts ok=false into an error with Kuma's
// message (bounded — Kuma messages are short, but never trust the peer).
func okOrError(raw json.RawMessage, endpoint string, secrets ...string) error {
	var res ackResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return errors.New("malformed acknowledgement")
	}
	if !res.OK {
		// Redact before truncation so a long reflected password cannot leave
		// a visible prefix that no longer matches the complete credential.
		msg := redact.ErrorWithURL(errors.New(res.Msg), endpoint, secrets...).Error()
		if len(msg) > 200 {
			msg = msg[:200]
		}
		return fmt.Errorf("kuma refused: %s", msg)
	}
	return nil
}

// addPayload builds the fields Kuma's UI sends for a new HTTP monitor; sending
// them keeps the created monitor identical to a hand-made one with defaults.
func addPayload(m Monitor) map[string]any {
	return map[string]any{
		"type":                 "http",
		"name":                 m.Name,
		"url":                  m.URL,
		"method":               "GET",
		"interval":             60,
		"retryInterval":        60,
		"resendInterval":       0,
		"maxretries":           1,
		"notificationIDList":   map[string]any{},
		"accepted_statuscodes": []string{"200-299"},
	}
}

// Add creates an HTTP monitor and returns its Kuma id.
func (s *Session) Add(ctx context.Context, m Monitor) (_ int64, err error) {
	defer func() { err = s.safeError(err) }()
	raw, err := s.emit(ctx, "add", addPayload(m))
	if err != nil {
		return 0, fmt.Errorf("add %q: %w", m.Name, err)
	}
	if err := okOrError(raw, s.endpoint, s.secrets...); err != nil {
		return 0, fmt.Errorf("add %q: %w", m.Name, err)
	}
	var res ackResult
	_ = json.Unmarshal(raw, &res) // Already validated above; never reflect malformed acknowledgements.
	if res.MonitorID <= 0 {
		return 0, fmt.Errorf("add %q: ack carried no positive monitorID", m.Name)
	}
	return res.MonitorID, nil
}

// Edit updates a monitor's name and url. The raw object from the monitorList
// is round-tripped so fields almanaut does not manage (interval, retries,
// notification assignments, ...) survive the edit.
func (s *Session) Edit(ctx context.Context, m Monitor) (err error) {
	defer func() { err = s.safeError(err) }()
	obj := make(map[string]any, len(m.raw)+3)
	for k, v := range m.raw {
		obj[k] = v
	}
	obj["id"] = m.ID
	obj["name"] = m.Name
	obj["url"] = m.URL
	raw, err := s.emit(ctx, "editMonitor", obj)
	if err != nil {
		return fmt.Errorf("edit monitor %d: %w", m.ID, err)
	}
	if err := okOrError(raw, s.endpoint, s.secrets...); err != nil {
		return fmt.Errorf("edit monitor %d: %w", m.ID, err)
	}
	return nil
}

// Delete removes a monitor by Kuma id.
func (s *Session) Delete(ctx context.Context, id int64) (err error) {
	defer func() { err = s.safeError(err) }()
	raw, err := s.emit(ctx, "deleteMonitor", id)
	if err != nil {
		return fmt.Errorf("delete monitor %d: %w", id, err)
	}
	if err := okOrError(raw, s.endpoint, s.secrets...); err != nil {
		return fmt.Errorf("delete monitor %d: %w", id, err)
	}
	return nil
}
