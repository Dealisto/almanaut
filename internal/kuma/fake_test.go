package kuma

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// fakeKuma is an in-process Uptime Kuma lookalike speaking just enough
// engine.io v4 / socket.io over websocket for the client and syncer tests.
type fakeKuma struct {
	t          *testing.T
	srv        *httptest.Server
	user, pass string

	mu       sync.Mutex
	monitors map[int64]map[string]any
	nextID   int64

	pongMu   sync.Mutex
	pongSeen int // count of "3" (pong) frames the client has sent back to us

	writeMu sync.Mutex // serializes writes once the login handler runs on its own goroutine
}

func newFakeKuma(t *testing.T) *fakeKuma {
	f := &fakeKuma{t: t, user: "admin", pass: "s3cret", monitors: map[int64]map[string]any{}, nextID: 1}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeKuma) url() string { return f.srv.URL }

func (f *fakeKuma) monitorCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.monitors)
}

// putMonitor seeds a monitor as if created by hand in Kuma; returns its id.
func (f *fakeKuma) putMonitor(name, url string) int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := f.nextID
	f.nextID++
	f.monitors[id] = map[string]any{"id": id, "type": "http", "name": name, "url": url}
	return id
}

func (f *fakeKuma) getMonitor(id int64) (map[string]any, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.monitors[id]
	return m, ok
}

// recordPong tallies a "3" (pong) frame received from the client. The login
// handler and tests use this to confirm the client actually answers our
// engine.io pings instead of ignoring them.
func (f *fakeKuma) recordPong() {
	f.pongMu.Lock()
	f.pongSeen++
	f.pongMu.Unlock()
}

// waitForPong polls (bounded by timeout) until at least n pongs have been
// recorded, returning false if the deadline passes first. No arbitrary
// sleeps: this is a condition-poll with a short interval and a hard cap.
func (f *fakeKuma) waitForPong(n int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		f.pongMu.Lock()
		ok := f.pongSeen >= n
		f.pongMu.Unlock()
		if ok {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (f *fakeKuma) handle(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, "/socket.io/") {
		http.NotFound(w, r)
		return
	}
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	ctx := r.Context()
	defer conn.CloseNow()

	// The login handler waits for a pong on its own goroutine (see below) so
	// the read loop stays free to receive that pong; writeMu keeps its ack
	// from racing with anything the read loop writes concurrently.
	write := func(s string) {
		f.writeMu.Lock()
		defer f.writeMu.Unlock()
		if err := conn.Write(ctx, websocket.MessageText, []byte(s)); err != nil {
			panic(http.ErrAbortHandler) // client went away; kill this handler
		}
	}
	write(`0{"sid":"fake","upgrades":[],"pingInterval":25000,"pingTimeout":20000}`)
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		msg := string(data)
		switch {
		case msg == "40": // socket.io connect
			write(`40{"sid":"fake"}`)
			// Ping right away: the login handler below requires the client
			// to have answered this before it will accept credentials, so
			// deleting the client's ping/pong handling fails the test.
			write("2")
		case msg == "3": // pong from the client
			f.recordPong()
		case strings.HasPrefix(msg, "42"):
			f.handleEvent(msg, write)
		}
	}
}

// handleEvent parses "42<ackId>["event",arg]" and answers "43<ackId>[result]".
func (f *fakeKuma) handleEvent(msg string, write func(string)) {
	rest := msg[2:]
	i := 0
	for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
		i++
	}
	ackID := rest[:i]
	var arr []json.RawMessage
	if err := json.Unmarshal([]byte(rest[i:]), &arr); err != nil || len(arr) == 0 {
		return
	}
	var event string
	_ = json.Unmarshal(arr[0], &event)
	ack := func(result string) {
		if ackID != "" {
			write("43" + ackID + "[" + result + "]")
		}
	}

	switch event {
	case "login":
		var creds struct{ Username, Password string }
		_ = json.Unmarshal(arr[1], &creds)
		if creds.Username != f.user || creds.Password != f.pass {
			ack(`{"ok":false,"msg":"Incorrect username or password."}`)
			return
		}
		// The pong wait runs on its own goroutine: handleEvent is called
		// synchronously from the read loop in handle() below, so blocking
		// here would stop that same loop from ever reading the pong frame
		// we're waiting for — a self-deadlock. Off-loading it lets the read
		// loop keep consuming frames (and recording pongs) concurrently.
		go func() {
			defer func() { recover() }() // conn may be gone by the time we write; don't crash the test binary
			if !f.waitForPong(1, 2*time.Second) {
				ack(`{"ok":false,"msg":"no pong received"}`)
				return
			}
			ack(`{"ok":true}`)
			f.mu.Lock()
			list, _ := json.Marshal(f.keyedMonitors())
			f.mu.Unlock()
			write(`42["monitorList",` + string(list) + `]`)
			// Second ping, post-login: exercises the read loop's pong
			// handling again on a well-established connection (as opposed
			// to the very first ping, which lands right at the login race).
			write("2")
		}()
	case "add":
		var m map[string]any
		_ = json.Unmarshal(arr[1], &m)
		f.mu.Lock()
		id := f.nextID
		f.nextID++
		m["id"] = id
		f.monitors[id] = m
		f.mu.Unlock()
		ack(fmt.Sprintf(`{"ok":true,"monitorID":%d}`, id))
	case "editMonitor":
		var m map[string]any
		_ = json.Unmarshal(arr[1], &m)
		id := int64(m["id"].(float64))
		f.mu.Lock()
		_, ok := f.monitors[id]
		if ok {
			f.monitors[id] = m
		}
		f.mu.Unlock()
		if !ok {
			ack(`{"ok":false,"msg":"monitor not found"}`)
			return
		}
		ack(fmt.Sprintf(`{"ok":true,"monitorID":%d}`, id))
	case "deleteMonitor":
		var id int64
		_ = json.Unmarshal(arr[1], &id)
		f.mu.Lock()
		delete(f.monitors, id)
		f.mu.Unlock()
		ack(`{"ok":true}`)
	}
}

// keyedMonitors returns monitors keyed by decimal id, as Kuma pushes them.
// Caller must hold f.mu.
func (f *fakeKuma) keyedMonitors() map[string]map[string]any {
	out := map[string]map[string]any{}
	for id, m := range f.monitors {
		out[strconv.FormatInt(id, 10)] = m
	}
	return out
}
