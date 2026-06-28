package discovery

import (
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func TestProposeServicesMapping(t *testing.T) {
	containers := []Container{{
		ID: "abc", Name: "jellyfin", Image: "jellyfin/jellyfin:latest",
		ComposeProject: "media",
		Ports:          []Port{{Public: 8096, Private: 8096, Proto: "tcp"}},
	}}
	got := ProposeServices(containers, nil)
	if len(got) != 1 {
		t.Fatalf("got %d proposals, want 1", len(got))
	}
	p := got[0]
	if p.ContainerID != "abc" {
		t.Errorf("ContainerID = %q", p.ContainerID)
	}
	if p.Service.Name != "jellyfin" || p.Service.Kind != "container" {
		t.Errorf("Service = %+v", p.Service)
	}
	if p.Service.Ports != "8096:8096/tcp" {
		t.Errorf("Ports = %q, want 8096:8096/tcp", p.Service.Ports)
	}
	if p.Service.Category != "media" {
		t.Errorf("Category = %q, want media", p.Service.Category)
	}
	if !strings.Contains(p.Service.Notes, "jellyfin/jellyfin:latest") {
		t.Errorf("Notes = %q, want image provenance", p.Service.Notes)
	}
	if p.AlreadyTracked {
		t.Error("AlreadyTracked = true, want false")
	}
}

func TestProposeServicesAlreadyTrackedCaseInsensitive(t *testing.T) {
	containers := []Container{{ID: "a", Name: "Jellyfin"}}
	existing := []domain.Service{{Name: "jellyfin"}}
	got := ProposeServices(containers, existing)
	if !got[0].AlreadyTracked {
		t.Error("expected case-insensitive already-tracked match")
	}
}

func TestProposeServicesEmptyPorts(t *testing.T) {
	got := ProposeServices([]Container{{ID: "a", Name: "x"}}, nil)
	if got[0].Service.Ports != "" {
		t.Errorf("Ports = %q, want empty", got[0].Service.Ports)
	}
}

func TestProposeServicesSortedByName(t *testing.T) {
	got := ProposeServices([]Container{
		{ID: "1", Name: "zeta"}, {ID: "2", Name: "alpha"},
	}, nil)
	if got[0].Service.Name != "alpha" || got[1].Service.Name != "zeta" {
		t.Errorf("not sorted by name: %q, %q", got[0].Service.Name, got[1].Service.Name)
	}
}

func TestProposeHostsMapping(t *testing.T) {
	scanned := []ScannedHost{{IP: "192.168.1.50", Hostname: "nas.lan", OpenPorts: []int{80, 443}}}
	got := ProposeHosts(scanned, nil)
	if len(got) != 1 {
		t.Fatalf("got %d proposals, want 1", len(got))
	}
	p := got[0]
	if p.IP != "192.168.1.50" {
		t.Errorf("IP = %q", p.IP)
	}
	if p.Host.Name != "nas.lan" {
		t.Errorf("Name = %q, want nas.lan", p.Host.Name)
	}
	if len(p.Host.IPs) != 1 || p.Host.IPs[0] != "192.168.1.50" {
		t.Errorf("IPs = %v", p.Host.IPs)
	}
	if p.Ports != "80, 443" {
		t.Errorf("Ports = %q, want \"80, 443\"", p.Ports)
	}
	if !strings.Contains(p.Host.Notes, "80, 443") {
		t.Errorf("Notes = %q, want open ports", p.Host.Notes)
	}
	if p.Host.Type != "" {
		t.Errorf("Type = %q, want empty (set at import)", p.Host.Type)
	}
}

func TestProposeHostsFallsBackToIPName(t *testing.T) {
	got := ProposeHosts([]ScannedHost{{IP: "10.0.0.9", OpenPorts: []int{22}}}, nil)
	if got[0].Host.Name != "10.0.0.9" {
		t.Errorf("Name = %q, want IP fallback", got[0].Host.Name)
	}
}

func TestProposeHostsAlreadyTrackedByIP(t *testing.T) {
	existing := []domain.Host{{Name: "nas", Type: "physical", IPs: []string{"192.168.1.50"}}}
	got := ProposeHosts([]ScannedHost{{IP: "192.168.1.50", OpenPorts: []int{80}}}, existing)
	if !got[0].AlreadyTracked {
		t.Error("expected already-tracked by IP")
	}
}

func TestProposeHostsSortedByIP(t *testing.T) {
	got := ProposeHosts([]ScannedHost{
		{IP: "192.168.1.10", OpenPorts: []int{80}},
		{IP: "192.168.1.2", OpenPorts: []int{80}},
	}, nil)
	if got[0].IP != "192.168.1.2" || got[1].IP != "192.168.1.10" {
		t.Errorf("not sorted by IP: %q, %q", got[0].IP, got[1].IP)
	}
}

func TestHumanBytes(t *testing.T) {
	cases := map[int64]string{
		512:        "512 B",
		4294967296: "4.0 GiB",
		536870912:  "512.0 MiB",
	}
	for in, want := range cases {
		if got := humanBytes(in); got != want {
			t.Errorf("humanBytes(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestProposeProxmoxHosts(t *testing.T) {
	res := []ProxmoxResource{
		{Type: "qemu", Node: "pve", Name: "web", Status: "running", ID: "qemu/100", MaxCPU: 4, MaxMem: 4294967296, MaxDisk: 34359738368},
		{Type: "node", Node: "pve", Status: "online", ID: "node/pve", MaxCPU: 8},
		{Type: "lxc", Node: "pve", Name: "dns", Status: "running", ID: "lxc/101", MaxCPU: 1},
	}
	existing := []domain.Host{{Name: "DNS", Type: "lxc"}} // case-insensitive match
	got := ProposeProxmoxHosts(res, existing)

	if len(got) != 3 {
		t.Fatalf("got %d proposals, want 3", len(got))
	}
	// Nodes sort first, then by name.
	if got[0].Host.Name != "pve" || got[0].Host.Type != "physical" {
		t.Errorf("first proposal = %+v, want node pve/physical", got[0].Host)
	}
	if got[0].Host.CPU != "8 cores" {
		t.Errorf("node CPU = %q, want %q", got[0].Host.CPU, "8 cores")
	}
	// Find the qemu proposal and check mapping.
	var web domain.Host
	var webTracked bool
	for _, p := range got {
		if p.ID == "qemu/100" {
			web, webTracked = p.Host, p.AlreadyTracked
		}
	}
	if web.Type != "vm" || web.Status != "running" || web.CPU != "4 cores" ||
		web.RAM != "4.0 GiB" || web.Disk != "32.0 GiB" || web.Notes != "Discovered from Proxmox." {
		t.Errorf("web host mapped wrong: %+v", web)
	}
	if webTracked {
		t.Error("web should not be already-tracked")
	}
	// dns matches existing host name case-insensitively.
	for _, p := range got {
		if p.ID == "lxc/101" && !p.AlreadyTracked {
			t.Error("dns should be already-tracked")
		}
	}
}
