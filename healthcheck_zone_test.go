package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestHealthcheckUsesScopedIPv6Address(t *testing.T) {
	listener, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skipf("IPv6 loopback unavailable: %v", err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			t.Errorf("unexpected healthcheck path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	server.Listener.Close()
	server.Listener = listener
	server.Start()
	defer server.Close()
	interfaces, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagLoopback == 0 {
			continue
		}
		port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
		for _, zone := range []string{iface.Name, strconv.Itoa(iface.Index)} {
			address := net.JoinHostPort("::1%"+zone, port)
			if got := runHealthcheck(address); got != 0 {
				t.Errorf("healthy scoped listener %s returned %d", address, got)
			}
		}
		return
	}
	t.Skip("no loopback interface available")
}
