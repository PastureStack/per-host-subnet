//go:build windows

package hostgw

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
	"strconv"

	"github.com/PastureStack/per-host-subnet/internal/metadata"
)

const managedWindowsRouteMetric = 45160

type windowsRoute struct {
	DestinationPrefix string `json:"DestinationPrefix"`
	InterfaceIndex    uint32 `json:"InterfaceIndex"`
	NextHop           string `json:"NextHop"`
	RouteMetric       uint32 `json:"RouteMetric"`
}

func (route windowsRoute) Equal(other windowsRoute) bool {
	return route.DestinationPrefix == other.DestinationPrefix && route.InterfaceIndex == other.InterfaceIndex &&
		net.ParseIP(route.NextHop).Equal(net.ParseIP(other.NextHop)) && route.RouteMetric == other.RouteMetric
}

type windowsRouteOperations interface {
	List(context.Context, uint32) (map[string]windowsRoute, error)
	Add(context.Context, windowsRoute) error
	Delete(context.Context, windowsRoute) error
}

type powerShellRouteOperations struct {
	run func(context.Context, string) ([]byte, error)
}

func newPowerShellRouteOperations() *powerShellRouteOperations {
	return &powerShellRouteOperations{run: func(ctx context.Context, script string) ([]byte, error) {
		return exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script).CombinedOutput()
	}}
}

func (operations *powerShellRouteOperations) List(ctx context.Context, interfaceIndex uint32) (map[string]windowsRoute, error) {
	script := "@(Get-NetRoute -AddressFamily IPv4 -PolicyStore ActiveStore -InterfaceIndex " + strconv.FormatUint(uint64(interfaceIndex), 10) +
		" -ErrorAction Stop | Where-Object { $_.RouteMetric -eq " + strconv.Itoa(managedWindowsRouteMetric) +
		" } | Select-Object DestinationPrefix,InterfaceIndex,NextHop,RouteMetric) | ConvertTo-Json -Compress"
	output, err := operations.run(ctx, script)
	if err != nil {
		return nil, fmt.Errorf("PowerShell Get-NetRoute failed: %w (%s)", err, bytes.TrimSpace(output))
	}
	return decodeWindowsRoutes(output)
}

func (operations *powerShellRouteOperations) Add(ctx context.Context, route windowsRoute) error {
	if err := validateWindowsRoute(route); err != nil {
		return err
	}
	script := "New-NetRoute -DestinationPrefix '" + route.DestinationPrefix + "' -InterfaceIndex " + strconv.FormatUint(uint64(route.InterfaceIndex), 10) +
		" -NextHop '" + route.NextHop + "' -RouteMetric " + strconv.Itoa(managedWindowsRouteMetric) + " -PolicyStore ActiveStore -ErrorAction Stop | Out-Null"
	if output, err := operations.run(ctx, script); err != nil {
		return fmt.Errorf("PowerShell New-NetRoute failed: %w (%s)", err, bytes.TrimSpace(output))
	}
	return nil
}

func (operations *powerShellRouteOperations) Delete(ctx context.Context, route windowsRoute) error {
	if err := validateWindowsRoute(route); err != nil {
		return err
	}
	script := "Remove-NetRoute -DestinationPrefix '" + route.DestinationPrefix + "' -InterfaceIndex " + strconv.FormatUint(uint64(route.InterfaceIndex), 10) +
		" -NextHop '" + route.NextHop + "' -PolicyStore ActiveStore -Confirm:$false -ErrorAction Stop"
	if output, err := operations.run(ctx, script); err != nil {
		return fmt.Errorf("PowerShell Remove-NetRoute failed: %w (%s)", err, bytes.TrimSpace(output))
	}
	return nil
}

func validateWindowsRoute(route windowsRoute) error {
	_, subnet, err := net.ParseCIDR(route.DestinationPrefix)
	if err != nil || subnet.IP.To4() == nil || route.InterfaceIndex == 0 {
		return fmt.Errorf("invalid managed Windows route")
	}
	nextHop := net.ParseIP(route.NextHop)
	if nextHop == nil || nextHop.To4() == nil {
		return fmt.Errorf("invalid managed Windows route next hop")
	}
	if route.RouteMetric != managedWindowsRouteMetric {
		return fmt.Errorf("managed Windows route has an unexpected metric")
	}
	return nil
}

func decodeWindowsRoutes(data []byte) (map[string]windowsRoute, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) || bytes.Equal(data, []byte("[]")) {
		return map[string]windowsRoute{}, nil
	}
	var routes []windowsRoute
	if data[0] == '[' {
		if err := json.Unmarshal(data, &routes); err != nil {
			return nil, fmt.Errorf("decode Windows routes: %w", err)
		}
	} else {
		var route windowsRoute
		if err := json.Unmarshal(data, &route); err != nil {
			return nil, fmt.Errorf("decode Windows route: %w", err)
		}
		routes = []windowsRoute{route}
	}
	result := make(map[string]windowsRoute, len(routes))
	for _, route := range routes {
		if err := validateWindowsRoute(route); err != nil {
			return nil, err
		}
		if existing, found := result[route.DestinationPrefix]; found && !existing.Equal(route) {
			return nil, fmt.Errorf("multiple managed Windows routes use destination %q", route.DestinationPrefix)
		}
		result[route.DestinationPrefix] = route
	}
	return result, nil
}

func desiredWindowsRoutes(selfHost metadata.Host, hosts []metadata.Host, interfaceIndex uint32) (map[string]windowsRoute, error) {
	result := map[string]windowsRoute{}
	for _, host := range hosts {
		if host.UUID == selfHost.UUID {
			continue
		}
		subnet, err := hostSubnetWindows(host)
		if err != nil {
			return nil, err
		}
		nextHop, err := hostRouterIP(host)
		if err != nil {
			return nil, err
		}
		candidate := windowsRoute{
			DestinationPrefix: subnet.String(),
			InterfaceIndex:    interfaceIndex,
			NextHop:           nextHop.String(),
			RouteMetric:       managedWindowsRouteMetric,
		}
		if existing, found := result[subnet.String()]; found && !existing.Equal(candidate) {
			return nil, fmt.Errorf("multiple hosts advertise per-host subnet %q", subnet.String())
		}
		result[subnet.String()] = candidate
	}
	return result, nil
}

func reconcileWindowsRoutes(ctx context.Context, operations windowsRouteOperations, current, desired, owned map[string]windowsRoute) error {
	for destination, previous := range owned {
		candidate, keep := desired[destination]
		if keep && previous.Equal(candidate) {
			continue
		}
		if existing, found := current[destination]; found {
			if !existing.Equal(previous) {
				return fmt.Errorf("owned Windows route %s was changed by another process", destination)
			}
			if err := operations.Delete(ctx, previous); err != nil {
				return fmt.Errorf("delete owned Windows route %s: %w", destination, err)
			}
			delete(current, destination)
		}
		delete(owned, destination)
	}

	for destination, candidate := range desired {
		if existing, found := current[destination]; found {
			if !existing.Equal(candidate) {
				return fmt.Errorf("Windows route destination %s is owned by a different route", destination)
			}
			continue
		}
		if err := operations.Add(ctx, candidate); err != nil {
			return fmt.Errorf("add managed Windows route %s: %w", destination, err)
		}
		owned[destination] = candidate
	}
	return nil
}
