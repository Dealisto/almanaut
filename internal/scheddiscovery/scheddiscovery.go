// Package scheddiscovery runs discovery sources on a schedule, records run
// history, and notifies about newly-appeared, not-yet-tracked findings. It does
// not import anything — findings are reviewed/imported via the manual pages.
package scheddiscovery

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/Dealisto/almanaut/internal/discovery"
	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/notify"
	"github.com/Dealisto/almanaut/internal/store"
)

type DockerScanner interface {
	Containers(ctx context.Context) ([]discovery.Container, error)
}
type NetworkScanner interface {
	Scan(ctx context.Context, cidr string, ports []int) ([]discovery.ScannedHost, error)
}
type ProxmoxScanner interface {
	Resources(ctx context.Context) ([]discovery.ProxmoxResource, error)
}
type ServiceLister interface {
	List() ([]domain.Service, error)
}
type HostLister interface {
	List() ([]domain.Host, error)
}

type Detector struct {
	docker   DockerScanner
	net      NetworkScanner
	subnet   string
	proxmox  ProxmoxScanner
	services ServiceLister
	hosts    HostLister
	runs     *store.DiscoveryRunRepo
	sender   notify.Sender
	log      *slog.Logger
	now      func() time.Time
}

func New(docker DockerScanner, net NetworkScanner, subnet string, proxmox ProxmoxScanner,
	services ServiceLister, hosts HostLister, runs *store.DiscoveryRunRepo,
	sender notify.Sender, log *slog.Logger, now func() time.Time) *Detector {
	if log == nil {
		log = slog.Default()
	}
	if now == nil {
		now = time.Now
	}
	return &Detector{docker, net, subnet, proxmox, services, hosts, runs, sender, log, now}
}

// FreshKeys returns keys in current not present in prev.
func FreshKeys(prev, current []string) []string {
	seen := make(map[string]bool, len(prev))
	for _, k := range prev {
		seen[k] = true
	}
	var out []string
	for _, k := range current {
		if !seen[k] {
			out = append(out, k)
		}
	}
	return out
}

func (d *Detector) RunDocker(ctx context.Context) error {
	start := d.now()
	var newKeys []string
	found := 0
	containers, scanErr := d.docker.Containers(ctx)
	if scanErr == nil {
		existing, err := d.services.List()
		if err != nil {
			scanErr = err
		} else {
			props := discovery.ProposeServices(containers, existing)
			found = len(props)
			for _, p := range props {
				if !p.AlreadyTracked {
					newKeys = append(newKeys, p.ContainerID)
				}
			}
		}
	}
	return d.finish(ctx, "docker", start, found, newKeys, scanErr)
}

func (d *Detector) RunNetwork(ctx context.Context) error {
	start := d.now()
	var newKeys []string
	found := 0
	scanned, scanErr := d.net.Scan(ctx, d.subnet, nil)
	if scanErr == nil {
		existing, err := d.hosts.List()
		if err != nil {
			scanErr = err
		} else {
			props := discovery.ProposeHosts(scanned, existing)
			found = len(props)
			for _, p := range props {
				if !p.AlreadyTracked {
					newKeys = append(newKeys, p.IP)
				}
			}
		}
	}
	return d.finish(ctx, "network", start, found, newKeys, scanErr)
}

func (d *Detector) RunProxmox(ctx context.Context) error {
	start := d.now()
	var newKeys []string
	found := 0
	res, scanErr := d.proxmox.Resources(ctx)
	if scanErr == nil {
		existing, err := d.hosts.List()
		if err != nil {
			scanErr = err
		} else {
			props := discovery.ProposeProxmoxHosts(res, existing)
			found = len(props)
			for _, p := range props {
				if !p.AlreadyTracked {
					newKeys = append(newKeys, p.ID)
				}
			}
		}
	}
	return d.finish(ctx, "proxmox", start, found, newKeys, scanErr)
}

// finish records the run and, on success with newly-appeared keys, notifies.
// It returns scanErr so the job runner surfaces a failed scan on the tasks page.
func (d *Detector) finish(ctx context.Context, source string, start time.Time, found int, newKeys []string, scanErr error) error {
	var fresh []string
	if scanErr == nil {
		prev, err := d.runs.LatestSuccessful(source)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			d.log.Error("discovery: read latest run", "source", source, "err", err)
		} else if err == nil {
			fresh = FreshKeys(prev.NewKeys, newKeys)
		} else {
			fresh = newKeys // first run: everything new is fresh
		}
	}

	errStr := ""
	if scanErr != nil {
		errStr = scanErr.Error()
	}
	if _, err := d.runs.Record(store.DiscoveryRun{
		Source: source, StartedAt: start, FinishedAt: d.now(),
		FoundCount: found, NewCount: len(newKeys), Error: errStr, NewKeys: newKeys,
	}); err != nil {
		d.log.Error("discovery: record run", "source", source, "err", err)
	}

	if scanErr == nil && len(fresh) > 0 {
		n := notify.Notification{
			Title: fmt.Sprintf("Discovery: %d new %s item(s) to review", len(fresh), source),
			Body:  fmt.Sprintf("Scheduled %s discovery found %d new item(s) not yet in the inventory. Review them on the Discovery page.", source, len(fresh)),
		}
		if err := d.sender.Send(ctx, n); err != nil {
			d.log.Error("discovery: send notification", "source", source, "err", err)
		}
	}
	return scanErr
}
