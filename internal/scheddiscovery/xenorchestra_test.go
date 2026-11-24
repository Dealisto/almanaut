package scheddiscovery

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/discovery"
	"github.com/Dealisto/almanaut/internal/domain"
)

type fakeXenOrchestra struct {
	resources []discovery.XenOrchestraResource
	err       error
}

func (f *fakeXenOrchestra) Resources(context.Context) ([]discovery.XenOrchestraResource, error) {
	return f.resources, f.err
}

type xoHostList struct {
	hosts []domain.Host
	err   error
}

func (h *xoHostList) List() ([]domain.Host, error) { return h.hosts, h.err }

func TestRunXenOrchestraFindingsAndRecovery(t *testing.T) {
	ctx := context.Background()
	runs := testStore(t)
	sender := &countSender{}
	scanner := &fakeXenOrchestra{resources: []discovery.XenOrchestraResource{
		{ID: "host-1", Type: "host", Name: "hypervisor"},
		{ID: "vm-1", Type: "vm", Name: "database", HostID: "host-1"},
		{ID: "vm-2", Type: "vm", Name: "ambiguous"},
		{ID: "vm-3", Type: "vm", Name: "AMBIGUOUS"},
	}}
	hosts := &xoHostList{hosts: []domain.Host{{Name: "HYPERVISOR", Type: "physical"}}}
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	detector := New(nil, nil, "", nil, nil, hosts, runs, sender, nil, func() time.Time { now = now.Add(time.Second); return now }, scanner)
	if err := detector.RunXenOrchestra(ctx); err != nil {
		t.Fatal(err)
	}
	run, err := runs.LatestSuccessful("xen-orchestra")
	if err != nil {
		t.Fatal(err)
	}
	if run.FoundCount != 4 || run.NewCount != 1 || len(run.NewKeys) != 1 || run.NewKeys[0] != "vm-1" {
		t.Fatalf("unexpected findings: %+v", run)
	}
	if sender.count() != 1 {
		t.Fatal("new finding should notify")
	}
	scanner.err = errors.New("source unavailable")
	if err := detector.RunXenOrchestra(ctx); !errors.Is(err, scanner.err) {
		t.Fatalf("scan error lost: %v", err)
	}
	failed, err := runs.Latest("xen-orchestra")
	if err != nil || failed.Error == "" {
		t.Fatalf("failed run not recorded: %+v, %v", failed, err)
	}
	scanner.err = nil
	if err := detector.RunXenOrchestra(ctx); err != nil {
		t.Fatal(err)
	}
	if sender.count() != 1 {
		t.Fatal("recovery must compare against last successful scan")
	}
	hosts.hosts = append(hosts.hosts, domain.Host{Name: "database", Type: "vm"})
	if err := detector.RunXenOrchestra(ctx); err != nil {
		t.Fatal(err)
	}
	run, err = runs.LatestSuccessful("xen-orchestra")
	if err != nil || run.NewCount != 0 {
		t.Fatalf("imported findings still new: %+v, %v", run, err)
	}
	scanner.resources = append(scanner.resources, discovery.XenOrchestraResource{ID: "vm-4", Type: "vm", Name: "new-service"})
	if err := detector.RunXenOrchestra(ctx); err != nil {
		t.Fatal(err)
	}
	if sender.count() != 2 {
		t.Fatal("new resource after recovery should notify")
	}
	if len(hosts.hosts) != 2 {
		t.Fatal("scheduled discovery must not import hosts")
	}
}

func TestRunXenOrchestraInventoryError(t *testing.T) {
	runs := testStore(t)
	sender := &countSender{}
	inventoryErr := errors.New("inventory unavailable")
	scanner := &fakeXenOrchestra{resources: []discovery.XenOrchestraResource{{ID: "vm-1", Type: "vm", Name: "guest"}}}
	d := New(nil, nil, "", nil, nil, &xoHostList{err: inventoryErr}, runs, sender, nil, nil, scanner)
	if err := d.RunXenOrchestra(context.Background()); !errors.Is(err, inventoryErr) {
		t.Fatalf("inventory error lost: %v", err)
	}
	if sender.count() != 0 {
		t.Fatal("failed inventory read must not notify")
	}
}
