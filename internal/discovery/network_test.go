package discovery

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestExpandCIDR(t *testing.T) {
	got, err := ExpandCIDR("192.168.1.0/30", 1024)
	if err != nil {
		t.Fatalf("ExpandCIDR /30: %v", err)
	}
	// /30 has 4 addresses; network and broadcast excluded -> 2 usable.
	want := []string{"192.168.1.1", "192.168.1.2"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("/30 = %v, want %v", got, want)
	}

	one, err := ExpandCIDR("10.0.0.5/32", 1024)
	if err != nil || len(one) != 1 || one[0] != "10.0.0.5" {
		t.Errorf("/32 = %v (err %v), want [10.0.0.5]", one, err)
	}

	if _, err := ExpandCIDR("10.0.0.0/8", 1024); err == nil {
		t.Error("oversized /8 should error")
	}
	if _, err := ExpandCIDR("not-a-cidr", 1024); err == nil {
		t.Error("invalid CIDR should error")
	}
}

func TestParsePorts(t *testing.T) {
	got, err := ParsePorts("22, 80,443")
	if err != nil {
		t.Fatalf("ParsePorts: %v", err)
	}
	if len(got) != 3 || got[0] != 22 || got[1] != 80 || got[2] != 443 {
		t.Errorf("got %v, want [22 80 443]", got)
	}
	if _, err := ParsePorts("22,abc"); err == nil {
		t.Error("non-numeric port should error")
	}
	if _, err := ParsePorts("0"); err == nil {
		t.Error("out-of-range port should error")
	}
}

func TestScanDetectsOpenPort(t *testing.T) {
	// A real listener on loopback gives a deterministic open port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	openPort := ln.Addr().(*net.TCPAddr).Port

	// Pick a almost-certainly-closed port (the listener's port + 1 is not bound).
	closedPort := openPort + 1

	s := &NetworkScanner{Ports: []int{openPort, closedPort}, Timeout: 300 * time.Millisecond, Concurrency: 4}
	hosts, err := s.Scan(context.Background(), "127.0.0.1/32", nil)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(hosts) != 1 {
		t.Fatalf("got %d hosts, want 1", len(hosts))
	}
	h := hosts[0]
	if h.IP != "127.0.0.1" {
		t.Errorf("IP = %q", h.IP)
	}
	if len(h.OpenPorts) != 1 || h.OpenPorts[0] != openPort {
		t.Errorf("OpenPorts = %v, want [%d]", h.OpenPorts, openPort)
	}
}

func TestScanRespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before scanning
	s := NewNetworkScanner()
	hosts, err := s.Scan(ctx, "192.168.250.0/24", nil)
	if err != nil {
		t.Fatalf("Scan should return cleanly on cancellation, got: %v", err)
	}
	if len(hosts) != 0 {
		t.Errorf("cancelled scan should find no hosts, got %d", len(hosts))
	}
}
