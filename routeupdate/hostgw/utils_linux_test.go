//go:build !windows

package hostgw

import (
	"net"
	"testing"

	"github.com/PastureStack/per-host-subnet/internal/metadata"
	"github.com/PastureStack/per-host-subnet/setting"
	"github.com/vishvananda/netlink"
)

type fakeRouteOperations struct {
	existing []netlink.Route
	replaced []*netlink.Route
	deleted  []*netlink.Route
	mask     uint64
	filter   *netlink.Route
}

func (fake *fakeRouteOperations) ListFiltered(_ int, filter *netlink.Route, mask uint64) ([]netlink.Route, error) {
	fake.filter = filter
	fake.mask = mask
	return fake.existing, nil
}

func (fake *fakeRouteOperations) Replace(route *netlink.Route) error {
	fake.replaced = append(fake.replaced, route)
	return nil
}

func (fake *fakeRouteOperations) Delete(route *netlink.Route) error {
	fake.deleted = append(fake.deleted, route)
	return nil
}

func TestDesiredRouteEntries(t *testing.T) {
	self := metadata.Host{UUID: "self", AgentIP: "192.0.2.10"}
	hosts := []metadata.Host{
		self,
		{UUID: "remote", AgentIP: "192.0.2.11", Labels: map[string]string{setting.PerHostSubnetLabel: "10.42.2.18/24"}},
	}
	desired, err := desiredRouteEntries(self, hosts)
	if err != nil {
		t.Fatal(err)
	}
	route := desired["10.42.2.0/24"]
	if route == nil || route.Gw.String() != "192.0.2.11" || route.Src.String() != "192.0.2.10" {
		t.Fatalf("route = %#v", route)
	}
	hosts[1].AgentIP = "invalid"
	if _, err := desiredRouteEntries(self, hosts); err == nil {
		t.Fatal("expected invalid remote agent IP to fail")
	}
	hosts = []metadata.Host{
		self,
		{UUID: "remote-a", AgentIP: "192.0.2.11", Labels: map[string]string{setting.PerHostSubnetLabel: "10.42.2.0/24"}},
		{UUID: "remote-b", AgentIP: "192.0.2.12", Labels: map[string]string{setting.PerHostSubnetLabel: "10.42.2.0/24"}},
	}
	if _, err := desiredRouteEntries(self, hosts); err == nil {
		t.Fatal("expected duplicate subnet ownership to fail")
	}
}

func TestCurrentAndUpdateOnlyMarkedRoutes(t *testing.T) {
	_, staleSubnet, _ := net.ParseCIDR("10.42.1.0/24")
	_, keepSubnet, _ := net.ParseCIDR("10.42.2.0/24")
	stale := netlink.Route{Dst: staleSubnet, Src: net.ParseIP("192.0.2.10"), Gw: net.ParseIP("192.0.2.20"), Table: 254, Priority: managedRoutePriority, Protocol: managedRouteProtocol}
	keep := netlink.Route{Dst: keepSubnet, Src: net.ParseIP("192.0.2.10"), Gw: net.ParseIP("192.0.2.21"), Table: 254, Priority: managedRoutePriority, Protocol: managedRouteProtocol}
	fake := &fakeRouteOperations{existing: []netlink.Route{stale, keep, {Dst: nil}}}
	original := routes
	routes = fake
	defer func() { routes = original }()

	current, err := currentRouteEntries(metadata.Host{UUID: "self", AgentIP: "192.0.2.10"})
	if err != nil {
		t.Fatal(err)
	}
	if len(current) != 2 || fake.mask != netlink.RT_FILTER_SRC|netlink.RT_FILTER_TABLE|netlink.RT_FILTER_PRIORITY|netlink.RT_FILTER_PROTOCOL {
		t.Fatalf("current=%v mask=%d", current, fake.mask)
	}
	desired := map[string]*netlink.Route{"10.42.2.0/24": &keep}
	if err := updateRoutes(current, desired); err != nil {
		t.Fatal(err)
	}
	if len(fake.deleted) != 1 || fake.deleted[0].Dst.String() != "10.42.1.0/24" || len(fake.replaced) != 0 {
		t.Fatalf("deleted=%v replaced=%v", fake.deleted, fake.replaced)
	}
}
