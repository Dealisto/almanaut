package discovery

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// defaultPorts is the common-service port set probed when none is supplied.
var defaultPorts = []int{22, 80, 139, 443, 445, 3389, 8006, 8080, 8443, 9000}

// maxScanHosts caps how many addresses a single scan will enumerate.
const maxScanHosts = 1024

// ScannedHost is a live host found by the network scan.
type ScannedHost struct {
	IP        string
	Hostname  string
	OpenPorts []int
}

// NetworkScanner performs a lightweight TCP-connect scan of a subnet.
type NetworkScanner struct {
	Ports       []int
	Timeout     time.Duration
	Concurrency int
}

// NewNetworkScanner returns a scanner with sensible defaults.
func NewNetworkScanner() *NetworkScanner {
	return &NetworkScanner{
		Ports:       defaultPorts,
		Timeout:     500 * time.Millisecond,
		Concurrency: 64,
	}
}

// Scan enumerates the subnet and returns hosts with at least one open port.
// ports overrides the scanner's default port set when non-empty.
func (s *NetworkScanner) Scan(ctx context.Context, cidr string, ports []int) ([]ScannedHost, error) {
	ips, err := ExpandCIDR(cidr, maxScanHosts)
	if err != nil {
		return nil, err
	}
	if len(ports) == 0 {
		ports = s.Ports
	}
	if len(ports) == 0 {
		ports = defaultPorts
	}
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = 500 * time.Millisecond
	}
	conc := s.Concurrency
	if conc <= 0 {
		conc = 64
	}

	dialer := &net.Dialer{Timeout: timeout}
	resolver := &net.Resolver{}
	jobs := make(chan string)
	results := make(chan ScannedHost)
	var wg sync.WaitGroup
	for i := 0; i < conc; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ip := range jobs {
				var open []int
				for _, p := range ports {
					conn, derr := dialer.DialContext(ctx, "tcp", net.JoinHostPort(ip, strconv.Itoa(p)))
					if derr == nil {
						open = append(open, p)
						_ = conn.Close()
					}
				}
				if len(open) == 0 {
					continue
				}
				sort.Ints(open)
				host := ScannedHost{IP: ip, OpenPorts: open}
				if names, lerr := resolver.LookupAddr(ctx, ip); lerr == nil && len(names) > 0 {
					host.Hostname = strings.TrimSuffix(names[0], ".")
				}
				results <- host
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, ip := range ips {
			select {
			case jobs <- ip:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		wg.Wait()
		close(results)
	}()

	var alive []ScannedHost
	for h := range results {
		alive = append(alive, h)
	}
	sort.Slice(alive, func(i, j int) bool { return ipLess(alive[i].IP, alive[j].IP) })
	return alive, nil
}

// ExpandCIDR returns the usable host addresses in cidr. For IPv4 prefixes
// shorter than /31 the network and broadcast addresses are excluded. It errors
// on an invalid CIDR or a range larger than maxHosts.
func ExpandCIDR(cidr string, maxHosts int) ([]string, error) {
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, fmt.Errorf("invalid CIDR %q: %w", cidr, err)
	}
	ones, bits := ipnet.Mask.Size()
	if ones == 0 && bits == 0 {
		return nil, fmt.Errorf("invalid CIDR mask %q", cidr)
	}
	hostBits := bits - ones
	if hostBits > 20 {
		return nil, fmt.Errorf("subnet %q is too large to scan", cidr)
	}
	base := ipnet.IP.Mask(ipnet.Mask)
	cur := make(net.IP, len(base))
	copy(cur, base)
	var ips []string
	for ipnet.Contains(cur) {
		ips = append(ips, cur.String())
		incIP(cur)
	}
	if ipnet.IP.To4() != nil && hostBits >= 2 && len(ips) >= 2 {
		ips = ips[1 : len(ips)-1] // drop network and broadcast
	}
	if len(ips) > maxHosts {
		return nil, fmt.Errorf("subnet %q has %d hosts, exceeds the %d limit", cidr, len(ips), maxHosts)
	}
	return ips, nil
}

// ParsePorts parses a comma-separated TCP port list (e.g. "22, 80, 443").
func ParsePorts(s string) ([]int, error) {
	var out []int
	for _, part := range strings.Split(s, ",") {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, fmt.Errorf("invalid port %q", p)
		}
		if n < 1 || n > 65535 {
			return nil, fmt.Errorf("port %d out of range", n)
		}
		out = append(out, n)
	}
	return out, nil
}

func incIP(ip net.IP) {
	for i := len(ip) - 1; i >= 0; i-- {
		ip[i]++
		if ip[i] != 0 {
			break
		}
	}
}

// ipLess orders IP strings by their parsed byte representation so that
// 192.168.1.2 precedes 192.168.1.10.
func ipLess(a, b string) bool {
	ia, ib := net.ParseIP(a), net.ParseIP(b)
	if ia == nil || ib == nil {
		return a < b
	}
	return bytes.Compare(ia.To16(), ib.To16()) < 0
}
