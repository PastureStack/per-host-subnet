//go:build !windows

package hostgw

import (
	"errors"
	"fmt"
	"net"

	"github.com/PastureStack/per-host-subnet/internal/metadata"
	"github.com/PastureStack/per-host-subnet/setting"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

const (
	managedRoutePriority = 45160
	managedRouteProtocol = netlink.RouteProtocol(99)
)

type routeOperations interface {
	ListFiltered(int, *netlink.Route, uint64) ([]netlink.Route, error)
	Replace(*netlink.Route) error
	Delete(*netlink.Route) error
}

type systemRouteOperations struct{}

func (systemRouteOperations) ListFiltered(family int, filter *netlink.Route, mask uint64) ([]netlink.Route, error) {
	return netlink.RouteListFiltered(family, filter, mask)
}

func (systemRouteOperations) Replace(route *netlink.Route) error { return netlink.RouteReplace(route) }
func (systemRouteOperations) Delete(route *netlink.Route) error  { return netlink.RouteDel(route) }

var routes routeOperations = systemRouteOperations{}

func hostAgentIP(host metadata.Host) (net.IP, error) {
	value := host.AgentIP
	if override := host.Labels[setting.OverrideAgentIPLabel]; override != "" {
		value = override
	}
	address := net.ParseIP(value)
	if address == nil || address.To4() == nil {
		return nil, fmt.Errorf("host %q has an invalid IPv4 agent address", host.UUID)
	}
	return address.To4(), nil
}

func hostSubnet(host metadata.Host) (*net.IPNet, error) {
	_, subnet, err := net.ParseCIDR(host.Labels[setting.PerHostSubnetLabel])
	if err != nil || subnet.IP.To4() == nil {
		return nil, fmt.Errorf("host %q has an invalid IPv4 per-host subnet", host.UUID)
	}
	return subnet, nil
}

func currentRouteEntries(selfHost metadata.Host) (map[string]*netlink.Route, error) {
	selfIP, err := hostAgentIP(selfHost)
	if err != nil {
		return nil, err
	}
	filter := &netlink.Route{
		Src:      selfIP,
		Table:    unix.RT_TABLE_MAIN,
		Priority: managedRoutePriority,
		Protocol: managedRouteProtocol,
	}
	mask := netlink.RT_FILTER_SRC | netlink.RT_FILTER_TABLE | netlink.RT_FILTER_PRIORITY | netlink.RT_FILTER_PROTOCOL
	existing, err := routes.ListFiltered(netlink.FAMILY_V4, filter, mask)
	if err != nil {
		return nil, err
	}
	entries := make(map[string]*netlink.Route, len(existing))
	for index := range existing {
		if existing[index].Dst == nil {
			continue
		}
		entries[existing[index].Dst.String()] = &existing[index]
	}
	return entries, nil
}

func desiredRouteEntries(selfHost metadata.Host, allHosts []metadata.Host) (map[string]*netlink.Route, error) {
	selfIP, err := hostAgentIP(selfHost)
	if err != nil {
		return nil, err
	}
	entries := map[string]*netlink.Route{}
	for _, host := range allHosts {
		if host.UUID == selfHost.UUID {
			continue
		}
		destination, err := hostSubnet(host)
		if err != nil {
			return nil, err
		}
		gateway, err := hostAgentIP(host)
		if err != nil {
			return nil, err
		}
		candidate := &netlink.Route{
			Dst:      destination,
			Src:      selfIP,
			Gw:       gateway,
			Table:    unix.RT_TABLE_MAIN,
			Priority: managedRoutePriority,
			Protocol: managedRouteProtocol,
		}
		if existing, found := entries[destination.String()]; found && !routesEqual(existing, candidate) {
			return nil, fmt.Errorf("multiple hosts advertise per-host subnet %q", destination.String())
		}
		entries[destination.String()] = candidate
	}
	return entries, nil
}

func updateRoutes(current, desired map[string]*netlink.Route) error {
	var operationErrors []error
	for destination, existing := range current {
		candidate, keep := desired[destination]
		if keep && routesEqual(existing, candidate) {
			delete(desired, destination)
			continue
		}
		if !keep {
			if err := routes.Delete(existing); err != nil {
				operationErrors = append(operationErrors, fmt.Errorf("delete route %s: %w", destination, err))
			}
		}
	}
	for destination, candidate := range desired {
		if err := routes.Replace(candidate); err != nil {
			operationErrors = append(operationErrors, fmt.Errorf("replace route %s: %w", destination, err))
		}
	}
	return errors.Join(operationErrors...)
}

func routesEqual(left, right *netlink.Route) bool {
	return left != nil && right != nil && left.Dst != nil && right.Dst != nil &&
		left.Dst.String() == right.Dst.String() && left.Src.Equal(right.Src) && left.Gw.Equal(right.Gw) &&
		left.Table == right.Table && left.Priority == right.Priority && left.Protocol == right.Protocol
}
