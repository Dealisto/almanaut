package snmp

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"
)

// This is a loopback protocol fixture, not a live-hardware compatibility test.
func TestSNMPUDPUsesOnlyReadRequests(t *testing.T) {
	server, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	var requests atomic.Int32
	done := make(chan error, 1)
	go func() {
		buffer := make([]byte, 65535)
		decoder := &gosnmp.GoSNMP{Version: gosnmp.Version2c, Community: "test-community"}
		for {
			n, peer, err := server.ReadFrom(buffer)
			if err != nil {
				done <- nil
				return
			}
			packet, err := decoder.SnmpDecodePacket(buffer[:n])
			if err != nil {
				done <- err
				return
			}
			if packet.PDUType != gosnmp.GetRequest && packet.PDUType != gosnmp.GetBulkRequest {
				done <- errors.New("collector issued a non-read PDU")
				return
			}
			requests.Add(1)
			packet.PDUType = gosnmp.GetResponse
			packet.Error = gosnmp.NoError
			packet.ErrorIndex = 0
			for i := range packet.Variables {
				if packet.Variables[i].Name == ".1.3.6.1.2.1.1.5.0" {
					packet.Variables[i].Type = gosnmp.OctetString
					packet.Variables[i].Value = []byte("fixture")
				} else {
					packet.Variables[i].Type = gosnmp.EndOfMibView
					packet.Variables[i].Value = nil
				}
			}
			wire, err := packet.MarshalMsg()
			if err != nil {
				done <- err
				return
			}
			if _, err := server.WriteTo(wire, peer); err != nil {
				done <- err
				return
			}
		}
	}()
	collector, err := NewCollector(Source{ID: "fixture", Name: "Fixture", Target: "127.0.0.1", Port: uint16(server.LocalAddr().(*net.UDPAddr).Port), Version: "2c", Community: "test-community"})
	if err != nil {
		t.Fatal(err)
	}
	observed, err := collector.Scan(context.Background())
	server.Close()
	serverErr := <-done
	if err != nil || serverErr != nil || observed.SystemName != "fixture" || requests.Load() < 10 {
		t.Fatalf("scan=%+v err=%v server=%v requests=%d", observed, err, serverErr, requests.Load())
	}
}
func TestSNMPUDPCancellationInterruptsBlockedRead(t *testing.T) {
	server, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	collector, err := NewCollector(Source{ID: "fixture", Name: "Fixture", Target: "127.0.0.1", Port: uint16(server.LocalAddr().(*net.UDPAddr).Port), Version: "2c", Community: "test-community"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := collector.Scan(ctx); done <- err }()
	if err := server.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := server.ReadFrom(make([]byte, 65535)); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if time.Since(started) > time.Second {
			t.Fatal("cancel did not interrupt request")
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled request remained blocked")
	}
}
