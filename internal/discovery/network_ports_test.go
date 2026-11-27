package discovery

import (
	"context"
	"net"
	"reflect"
	"testing"
)

func TestParsePortsTreatsRepeatedNumbersAsOnePort(t *testing.T) {
	ports, err := ParsePorts("443,22, 00443, ,22,65535,1,1")
	if err != nil || !reflect.DeepEqual(ports, []int{443, 22, 65535, 1}) {
		t.Fatalf("ports=%v error=%v", ports, err)
	}
}

func TestNetworkScanDeduplicatesOverrideAndConfiguredPorts(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	for _, override := range []bool{false, true} {
		s := NewNetworkScanner()
		ports := []int{port, port, port}
		if !override {
			s.Ports, ports = ports, nil
		}
		hosts, err := s.Scan(context.Background(), "127.0.0.1/32", ports)
		if err != nil || len(hosts) != 1 || !reflect.DeepEqual(hosts[0].OpenPorts, []int{port}) {
			t.Fatalf("override=%v: hosts=%+v error=%v", override, hosts, err)
		}
	}
}

func TestNetworkScanRejectsInvalidPorts(t *testing.T) {
	for _, port := range []int{-1, 0, 65536} {
		for _, override := range []bool{false, true} {
			s := NewNetworkScanner()
			ports := []int{port}
			if !override {
				s.Ports, ports = ports, nil
			}
			if hosts, err := s.Scan(context.Background(), "127.0.0.1/32", ports); err == nil || len(hosts) != 0 {
				t.Fatalf("invalid port %d produced a successful scan: hosts=%v err=%v", port, hosts, err)
			}
		}
	}
}
