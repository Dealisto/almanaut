package snmp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/gosnmp/gosnmp"
)

const ifBase = ".1.3.6.1.2.1.2.2.1."
const ifXBase = ".1.3.6.1.2.1.31.1.1.1."
const entityBase = ".1.3.6.1.2.1.47.1.1.1.1."
const localBase = ".1.0.8802.1.1.2.1.3.7.1."
const remoteBase = ".1.0.8802.1.1.2.1.4.1.1."

var errLimit = errors.New("SNMP observation exceeds its bounded collection limit")

type transport interface {
	Connect() error
	Close() error
	Get([]string) (*gosnmp.SnmpPacket, error)
	BulkWalk(string, gosnmp.WalkFunc) error
}

type Collector struct {
	source Source
	open   func(context.Context) transport
}

func NewCollector(source Source) (*Collector, error) {
	if err := source.Validate(); err != nil {
		return nil, err
	}
	c := &Collector{source: source}
	c.open = func(ctx context.Context) transport { return client(source, ctx) }
	return c, nil
}

func client(s Source, ctx context.Context) *gosnmp.GoSNMP {
	c := &gosnmp.GoSNMP{Target: s.Target, Port: s.Port, Transport: "udp", Context: ctx, Timeout: 2 * time.Second, Retries: 1, MaxOids: 10, MaxRepetitions: 25, Version: gosnmp.Version3, MsgFlags: gosnmp.AuthPriv, SecurityModel: gosnmp.UserSecurityModel, ContextName: s.ContextName}
	if s.Version == "2c" {
		c.Version = gosnmp.Version2c
		c.Community = s.Community
		c.MsgFlags = 0
		c.SecurityModel = 0
	} else {
		c.SecurityParameters = &gosnmp.UsmSecurityParameters{UserName: s.Username, AuthenticationProtocol: gosnmp.SHA256, AuthenticationPassphrase: s.AuthPassword, PrivacyProtocol: gosnmp.AES, PrivacyPassphrase: s.PrivacyPassword}
	}
	return c
}

// Scan uses only GET and GETBULK. Unsupported optional MIBs produce warnings;
// incomplete interface tables fail instead of presenting a partial import.
func (c *Collector) Scan(parent context.Context) (Observation, error) {
	ctx, cancel := context.WithTimeout(parent, ScanTimeout)
	defer cancel()
	conn := c.open(ctx)
	if err := conn.Connect(); err != nil {
		return Observation{}, scanError(ctx, "connect")
	}
	defer conn.Close()
	// A connected UDP socket can otherwise remain blocked until its deadline.
	if live, ok := conn.(*gosnmp.GoSNMP); ok {
		socket := live.Conn
		// Close the socket, not GoSNMP.Conn itself: the library reads that
		// field without a mutex during requests. net.Conn.Close is concurrent-safe.
		stop := context.AfterFunc(ctx, func() { _ = socket.Close() })
		defer stop()
	}
	var out Observation
	packet, err := conn.Get([]string{".1.3.6.1.2.1.1.1.0", ".1.3.6.1.2.1.1.2.0", ".1.3.6.1.2.1.1.5.0"})
	if err != nil || packet == nil || packet.Error != gosnmp.NoError {
		return out, scanError(ctx, "read system identity")
	}
	for _, v := range packet.Variables {
		switch v.Name {
		case ".1.3.6.1.2.1.1.1.0":
			out.Description = display(v.Value)
		case ".1.3.6.1.2.1.1.2.0":
			out.ObjectID = display(v.Value)
		case ".1.3.6.1.2.1.1.5.0":
			out.SystemName = display(v.Value)
		}
	}
	used := 0
	walk := func(base string, columns []int, limit int) (map[string]map[int]any, error) {
		rows := map[string]map[int]any{}
		for _, column := range columns {
			root := base + strconv.Itoa(column)
			err := conn.BulkWalk(root, func(v gosnmp.SnmpPDU) error {
				if err := ctx.Err(); err != nil {
					return err
				}
				used++
				if used > 20000 {
					return errLimit
				}
				if v.Type == gosnmp.NoSuchObject || v.Type == gosnmp.NoSuchInstance || v.Type == gosnmp.EndOfMibView {
					return nil
				}
				if !strings.HasPrefix(v.Name, root+".") {
					return fmt.Errorf("unexpected MIB response")
				}
				key := strings.TrimPrefix(v.Name, root+".")
				if len(key) > 128 {
					return errLimit
				}
				if rows[key] == nil {
					if len(rows) >= limit {
						return errLimit
					}
					rows[key] = map[int]any{}
				}
				if b, ok := v.Value.([]byte); ok && len(b) > 1024 {
					return errLimit
				}
				if s, ok := v.Value.(string); ok && len(s) > 1024 {
					return errLimit
				}
				rows[key][column] = v.Value
				return nil
			})
			if err != nil {
				return nil, err
			}
		}
		return rows, nil
	}
	interfaces, err := walk(ifBase, []int{2, 3, 5, 6, 7, 8}, MaxInterfaces)
	if err != nil {
		return Observation{}, scanError(ctx, "read complete interface table")
	}
	extended, err := walk(ifXBase, []int{1, 15, 18}, MaxInterfaces)
	if err != nil {
		extended = nil
		out.Warnings = append(out.Warnings, "Extended interface details unavailable.")
	}
	for key, row := range interfaces {
		index, e := strconv.Atoi(key)
		if e != nil || index < 1 {
			return Observation{}, scanError(ctx, "decode interface indexes")
		}
		i := Interface{Index: index, Description: display(row[2]), Type: integer(row[3]), MAC: mac(row[6]), SpeedMbps: uint64(max(integer(row[5]), 0)) / 1000000, AdminStatus: integer(row[7]), OperStatus: integer(row[8])}
		if x := extended[key]; x != nil {
			i.Name = display(x[1])
			i.Alias = display(x[18])
			if high := integer(x[15]); high > 0 {
				i.SpeedMbps = uint64(high)
			}
		}
		if i.Name == "" {
			i.Name = i.Description
		}
		out.Interfaces = append(out.Interfaces, i)
	}
	sort.Slice(out.Interfaces, func(i, j int) bool { return out.Interfaces[i].Index < out.Interfaces[j].Index })
	physical, err := walk(entityBase, []int{5, 11, 12, 13}, MaxInterfaces)
	if err != nil {
		out.Warnings = append(out.Warnings, "Chassis details unavailable.")
	} else {
		var chassis []map[int]any
		for _, row := range physical {
			if integer(row[5]) == 3 {
				chassis = append(chassis, row)
			}
		}
		if len(chassis) == 1 {
			out.Serial = display(chassis[0][11])
			out.Manufacturer = display(chassis[0][12])
			out.Model = display(chassis[0][13])
		} else if len(chassis) > 1 {
			out.Warnings = append(out.Warnings, "Multiple chassis reported; no single chassis identity selected.")
		}
	}
	locals, localErr := walk(localBase, []int{2, 3, 4}, MaxInterfaces)
	remotes, remoteErr := walk(remoteBase, []int{4, 5, 6, 7, 8, 9}, MaxNeighbors)
	if localErr != nil || remoteErr != nil {
		out.Warnings = append(out.Warnings, "LLDP unavailable or incomplete; observed links cannot be applied.")
	} else {
		out.LLDPComplete = true
		seen := map[string]bool{}
		for key, row := range remotes {
			parts := strings.Split(key, ".")
			if len(parts) != 3 {
				out.LLDPComplete = false
				break
			}
			local, e := strconv.Atoi(parts[1])
			if e != nil || local < 1 {
				out.LLDPComplete = false
				break
			}
			loc := locals[parts[1]]
			n := Neighbor{LocalNumber: local, LocalSubtype: integer(loc[2]), LocalID: identity(loc[3], integer(loc[2]) == 3), LocalDescription: display(loc[4]), ChassisSubtype: integer(row[4]), ChassisID: identity(row[5], integer(row[4]) == 4), PortSubtype: integer(row[6]), PortID: identity(row[7], integer(row[6]) == 3), PortDescription: display(row[8]), SystemName: display(row[9])}
			if n.ChassisID == "" || n.PortID == "" || n.LocalID == "" || n.LocalSubtype < 1 || n.LocalSubtype > 7 || n.ChassisSubtype < 1 || n.ChassisSubtype > 7 || n.PortSubtype < 1 || n.PortSubtype > 7 {
				out.LLDPComplete = false
				break
			}
			key := Fingerprint(n)
			if !seen[key] {
				out.Neighbors = append(out.Neighbors, n)
				seen[key] = true
			}
		}
		if !out.LLDPComplete {
			out.Neighbors = nil
			out.Warnings = append(out.Warnings, "LLDP identity records incomplete; observed links cannot be applied.")
		}
		sort.Slice(out.Neighbors, func(i, j int) bool { return Fingerprint(out.Neighbors[i]) < Fingerprint(out.Neighbors[j]) })
	}
	if ctx.Err() != nil {
		return Observation{}, ctx.Err()
	}
	return out, nil
}

func scanError(ctx context.Context, stage string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return fmt.Errorf("SNMP could not %s; check device access, credentials and supported MIBs", stage)
}
func integer(v any) int {
	if v == nil {
		return 0
	}
	return int(gosnmp.ToBigInt(v).Int64())
}
func mac(v any) string {
	b, ok := v.([]byte)
	if !ok || (len(b) != 6 && len(b) != 8) {
		return ""
	}
	return net.HardwareAddr(b).String()
}
func identity(v any, isMAC bool) string {
	if isMAC {
		if m := mac(v); m != "" {
			return m
		}
	}
	if b, ok := v.([]byte); ok {
		for _, c := range b {
			if c < 32 || c == 127 {
				return fmt.Sprintf("%x", b)
			}
		}
	}
	return display(v)
}
func display(v any) string {
	var s string
	switch v := v.(type) {
	case nil:
		return ""
	case []byte:
		s = string(v)
	case string:
		s = v
	default:
		s = fmt.Sprint(v)
	}
	s = strings.ToValidUTF8(s, "�")
	if len(s) > 1024 {
		s = string([]rune(s)[:min(256, len([]rune(s)))])
	}
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s))
}
