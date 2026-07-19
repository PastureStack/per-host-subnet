//go:build windows

package hostgw

import (
	"context"
	"testing"

	"github.com/PastureStack/per-host-subnet/internal/metadata"
	"github.com/PastureStack/per-host-subnet/setting"
)

type fakeWindowsRoutes struct {
	added   []windowsRoute
	deleted []windowsRoute
}

func (*fakeWindowsRoutes) List(context.Context, uint32) (map[string]windowsRoute, error) {
	return nil, nil
}
func (fake *fakeWindowsRoutes) Add(_ context.Context, route windowsRoute) error {
	fake.added = append(fake.added, route)
	return nil
}
func (fake *fakeWindowsRoutes) Delete(_ context.Context, route windowsRoute) error {
	fake.deleted = append(fake.deleted, route)
	return nil
}

func TestDecodeWindowsRoutes(t *testing.T) {
	data := []byte(`[{"DestinationPrefix":"10.42.1.0/24","InterfaceIndex":7,"NextHop":"192.0.2.11","RouteMetric":45160}]`)
	routes, err := decodeWindowsRoutes(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 1 || routes["10.42.1.0/24"].InterfaceIndex != 7 {
		t.Fatalf("routes = %#v", routes)
	}
	if _, err := decodeWindowsRoutes([]byte(`{"DestinationPrefix":"invalid","InterfaceIndex":7,"NextHop":"192.0.2.11","RouteMetric":45160}`)); err == nil {
		t.Fatal("expected invalid route to fail")
	}
}

func TestDesiredAndUpdateWindowsRoutes(t *testing.T) {
	self := metadata.Host{UUID: "self"}
	hosts := []metadata.Host{
		self,
		{UUID: "remote", Labels: map[string]string{setting.PerHostSubnetLabel: "10.42.1.8/24", setting.RouterIPLabel: "192.0.2.11"}},
	}
	desired, err := desiredWindowsRoutes(self, hosts, 7)
	if err != nil {
		t.Fatal(err)
	}
	if desired["10.42.1.0/24"].NextHop != "192.0.2.11" {
		t.Fatalf("desired = %#v", desired)
	}
	duplicateHosts := append(hosts, metadata.Host{UUID: "remote-b", Labels: map[string]string{
		setting.PerHostSubnetLabel: "10.42.1.0/24", setting.RouterIPLabel: "192.0.2.12",
	}})
	if _, err := desiredWindowsRoutes(self, duplicateHosts, 7); err == nil {
		t.Fatal("expected duplicate subnet ownership to fail")
	}
	stale := windowsRoute{DestinationPrefix: "10.42.2.0/24", InterfaceIndex: 7, NextHop: "192.0.2.12", RouteMetric: managedWindowsRouteMetric}
	fake := &fakeWindowsRoutes{}
	owned := map[string]windowsRoute{stale.DestinationPrefix: stale}
	if err := reconcileWindowsRoutes(context.Background(), fake, map[string]windowsRoute{stale.DestinationPrefix: stale}, desired, owned); err != nil {
		t.Fatal(err)
	}
	if len(fake.deleted) != 1 || len(fake.added) != 1 {
		t.Fatalf("deleted=%v added=%v", fake.deleted, fake.added)
	}
	if len(owned) != 1 || owned["10.42.1.0/24"].NextHop != "192.0.2.11" {
		t.Fatalf("owned = %#v", owned)
	}
}

func TestReconcileWindowsRoutesDoesNotAdoptExistingRoute(t *testing.T) {
	existing := windowsRoute{DestinationPrefix: "10.42.1.0/24", InterfaceIndex: 7, NextHop: "192.0.2.11", RouteMetric: managedWindowsRouteMetric}
	fake := &fakeWindowsRoutes{}
	owned := map[string]windowsRoute{}
	if err := reconcileWindowsRoutes(context.Background(), fake,
		map[string]windowsRoute{existing.DestinationPrefix: existing},
		map[string]windowsRoute{existing.DestinationPrefix: existing}, owned); err != nil {
		t.Fatal(err)
	}
	if len(fake.added) != 0 || len(fake.deleted) != 0 || len(owned) != 0 {
		t.Fatalf("existing route must remain unowned: added=%v deleted=%v owned=%v", fake.added, fake.deleted, owned)
	}
}
