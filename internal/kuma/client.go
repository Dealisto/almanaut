package kuma

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"

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
func (c *Client) Connect(ctx context.Context) (*Session, error) {
	httpClient := http.DefaultClient
	if c.insecure {
		httpClient = &http.Client{Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}}
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
	if err != nil || !strings.HasPrefix(frame, "0") {
		conn.CloseNow()
		return nil, fmt.Errorf("kuma: engine.io open: %v (frame %.32q)", err, frame)
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
	if err := okOrError(res); err != nil {
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
			s.handleEvent(msg)
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
func (s *Session) handleEvent(msg string) {
	rest := strings.TrimLeft(msg[2:], "0123456789")
	var arr []json.RawMessage
	if err := json.Unmarshal([]byte(rest), &arr); err != nil || len(arr) < 2 {
		return
	}
	var event string
	if err := json.Unmarshal(arr[0], &event); err != nil || event != "monitorList" {
		return
	}
	var list map[string]map[string]any
	if err := json.Unmarshal(arr[1], &list); err != nil {
		return
	}
	mons := make(map[int64]Monitor, len(list))
	for key, raw := range list {
		id, err := strconv.ParseInt(key, 10, 64)
		if err != nil {
			continue
		}
		m := Monitor{ID: id, raw: raw}
		if v, ok := raw["name"].(string); ok {
			m.Name = v
		}
		if v, ok := raw["url"].(string); ok {
			m.URL = v
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
func okOrError(raw json.RawMessage) error {
	var res ackResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return fmt.Errorf("malformed ack %.128q", string(raw))
	}
	if !res.OK {
		msg := res.Msg
		if len(msg) > 200 {
			msg = msg[:200]
		}
		return fmt.Errorf("kuma refused: %s", msg)
	}
	return nil
}
