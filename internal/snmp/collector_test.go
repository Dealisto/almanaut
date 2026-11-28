package snmp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gosnmp/gosnmp"
)

type fixtureTransport struct {
	rows   map[string][]gosnmp.SnmpPDU
	fail   string
	closed atomic.Bool
	onWalk func()
}

func (f *fixtureTransport) Connect() error { return nil }
func (f *fixtureTransport) Close() error   { f.closed.Store(true); return nil }
func (f *fixtureTransport) Get(_ []string) (*gosnmp.SnmpPacket, error) {
	return &gosnmp.SnmpPacket{Variables: []gosnmp.SnmpPDU{{Name: ".1.3.6.1.2.1.1.5.0", Value: []byte("switch <script>")}}}, nil
}
func (f *fixtureTransport) BulkWalk(root string, fn gosnmp.WalkFunc) error {
	if f.onWalk != nil {
		f.onWalk()
	}
	if root == f.fail {
		return errors.New("secret-community must never escape")
	}
	for _, p := range f.rows[root] {
		if err := fn(p); err != nil {
			return err
		}
	}
	return nil
}
func (f *fixtureTransport) add(root, key string, value any) {
	f.rows[root] = append(f.rows[root], gosnmp.SnmpPDU{Name: root + "." + key, Type: gosnmp.OctetString, Value: value})
}
func fixture() *fixtureTransport {
	f := &fixtureTransport{rows: map[string][]gosnmp.SnmpPDU{}}
	f.add(ifBase+"2", "7", []byte("Ethernet seven"))
	f.add(ifBase+"3", "7", 6)
	f.add(ifBase+"6", "7", []byte{0, 1, 2, 3, 4, 5})
	f.add(ifXBase+"1", "7", []byte("eth7"))
	f.add(ifXBase+"15", "7", 10000)
	f.add(entityBase+"5", "1", 3)
	f.add(entityBase+"11", "1", []byte("serial"))
	f.add(entityBase+"12", "1", []byte("manufacturer"))
	f.add(entityBase+"13", "1", []byte("model"))
	f.add(localBase+"2", "19", 5)
	f.add(localBase+"3", "19", []byte("ethernet-7"))
	f.add(localBase+"4", "19", []byte("uplink"))
	f.add(remoteBase+"4", "41.19.1", 4)
	f.add(remoteBase+"5", "41.19.1", []byte{0, 1, 2, 3, 4, 6})
	f.add(remoteBase+"6", "41.19.1", 5)
	f.add(remoteBase+"7", "41.19.1", []byte("remote-port"))
	f.add(remoteBase+"9", "41.19.1", []byte("remote-name"))
	return f
}
func collectFixture(ctx context.Context, f *fixtureTransport) (Observation, error) {
	return (&Collector{open: func(context.Context) transport { return f }}).Scan(ctx)
}
func TestSNMPObservationPreservesIndependentLLDPIndexes(t *testing.T) {
	f := fixture()
	o, err := collectFixture(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	if !f.closed.Load() || len(o.Interfaces) != 1 || o.Interfaces[0].Index != 7 || o.Interfaces[0].Name != "eth7" || o.Interfaces[0].SpeedMbps != 10000 || o.Interfaces[0].MAC != "00:01:02:03:04:05" {
		t.Fatalf("interfaces: %+v", o)
	}
	if !o.LLDPComplete || len(o.Neighbors) != 1 || o.Neighbors[0].LocalNumber != 19 || o.Neighbors[0].LocalID != "ethernet-7" || o.Neighbors[0].ChassisID != "00:01:02:03:04:06" {
		t.Fatalf("neighbors: %+v", o)
	}
	if o.Model != "model" || o.Serial != "serial" {
		t.Fatalf("chassis: %+v", o)
	}
	// LLDP table time marks change without changing the reviewed relationship.
	g := fixture()
	for root, rows := range g.rows {
		if strings.HasPrefix(root, remoteBase) {
			for i := range rows {
				rows[i].Name = strings.Replace(rows[i].Name, ".41.19.1", ".999.19.1", 1)
			}
			g.rows[root] = rows
		}
	}
	other, err := collectFixture(context.Background(), g)
	if err != nil || Fingerprint(other.Neighbors) != Fingerprint(o.Neighbors) {
		t.Fatalf("volatile indexes changed identity: %v", err)
	}
}
func TestSNMPBoundsFailureAndIncompleteLLDP(t *testing.T) {
	t.Run("interfaces", func(t *testing.T) {
		f := fixture()
		for i := 1; i <= MaxInterfaces+1; i++ {
			f.add(ifBase+"2", fmt.Sprint(i), []byte("eth"))
		}
		_, err := collectFixture(context.Background(), f)
		if err == nil {
			t.Fatal("accepted oversized table")
		}
	})
	t.Run("required error redacted", func(t *testing.T) {
		f := fixture()
		f.fail = ifBase + "2"
		_, err := collectFixture(context.Background(), f)
		if err == nil || strings.Contains(err.Error(), "secret-community") {
			t.Fatalf("error %v", err)
		}
	})
	t.Run("incomplete neighbors", func(t *testing.T) {
		f := fixture()
		f.fail = remoteBase + "7"
		o, err := collectFixture(context.Background(), f)
		if err != nil || o.LLDPComplete || len(o.Neighbors) != 0 || len(o.Warnings) == 0 {
			t.Fatalf("incomplete accepted: %+v %v", o, err)
		}
	})
	t.Run("missing neighbor subtype", func(t *testing.T) {
		f := fixture()
		delete(f.rows, remoteBase+"6")
		o, err := collectFixture(context.Background(), f)
		if err != nil || o.LLDPComplete || len(o.Neighbors) != 0 {
			t.Fatalf("incomplete subtype accepted: %+v %v", o, err)
		}
	})
	t.Run("ambiguous chassis", func(t *testing.T) {
		f := fixture()
		f.add(entityBase+"5", "2", 3)
		o, err := collectFixture(context.Background(), f)
		if err != nil || o.Serial != "" || o.Model != "" {
			t.Fatalf("guessed chassis: %+v %v", o, err)
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := collectFixture(ctx, fixture())
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel: %v", err)
		}
	})
}
func TestSNMPv3RequiresAuthenticationAndPrivacy(t *testing.T) {
	s := Source{ID: "switch", Name: "Switch", Target: "192.0.2.1", Port: 161, Version: "3", Username: "reader", AuthPassword: "auth-secret", PrivacyPassword: "priv-secret"}
	if _, err := NewCollector(s); err != nil {
		t.Fatal(err)
	}
	c := client(s, context.Background())
	u := c.SecurityParameters.(*gosnmp.UsmSecurityParameters)
	if c.Version != gosnmp.Version3 || c.MsgFlags != gosnmp.AuthPriv || u.AuthenticationProtocol != gosnmp.SHA256 || u.PrivacyProtocol != gosnmp.AES || c.Retries != 1 {
		t.Fatalf("insecure client options")
	}
	s.PrivacyPassword = ""
	if _, err := NewCollector(s); err == nil {
		t.Fatal("accepted missing privacy")
	}
}
