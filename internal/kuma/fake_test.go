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

	write := func(s string) {
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
		case msg == "3": // pong — ignore
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
		ack(`{"ok":true}`)
		f.mu.Lock()
		list, _ := json.Marshal(f.keyedMonitors())
		f.mu.Unlock()
		write(`42["monitorList",` + string(list) + `]`)
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
