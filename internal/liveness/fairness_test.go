package liveness

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
)

type serviceList []domain.Service

func (s serviceList) List() ([]domain.Service, error) { return []domain.Service(s), nil }

func TestCheckerInterruptedPassesReachHostsAndServices(t *testing.T) {
	hosts := hostList{
		{ID: 2, Name: "a", CheckAddress: "host2:443"},
		{ID: 1, Name: "z", CheckAddress: "host1:443"},
	}
	services := serviceList{{ID: 1, Name: "service", CheckAddress: "service1:443"}}
	var cancel context.CancelFunc
	var visited []string
	checker := New(hosts, services, newFakeState(), &countingSender{},
		func(ctx context.Context, address string) error {
			visited = append(visited, address)
			cancel()
			return ctx.Err()
		}, time.Second, nil, nil)
	checker.workers = 1
	for range 4 {
		var ctx context.Context
		ctx, cancel = context.WithCancel(context.Background())
		err := checker.Run(ctx)
		cancel()
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("interrupted pass error = %v, want context.Canceled", err)
		}
	}
	want := []string{"host1:443", "host2:443", "service1:443", "host1:443"}
	if !slices.Equal(visited, want) {
		t.Fatalf("visited = %v, want %v", visited, want)
	}
}

func TestCheckerRotationSurvivesRemovedTarget(t *testing.T) {
	hosts := hostList{
		{ID: 1, Name: "first", CheckAddress: "first:443"},
		{ID: 2, Name: "disabled", CheckAddress: ""},
		{ID: 3, Name: "last", CheckAddress: "last:443"},
	}
	var visited []string
	checker := New(hosts, noServices{}, newFakeState(), &countingSender{},
		func(_ context.Context, address string) error {
			visited = append(visited, address)
			return nil
		}, time.Second, nil, nil)
	checker.workers = 1
	checker.nextType, checker.nextID = "host", 2
	if err := checker.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if want := []string{"last:443", "first:443"}; !slices.Equal(visited, want) {
		t.Fatalf("visited = %v, want %v", visited, want)
	}
}
