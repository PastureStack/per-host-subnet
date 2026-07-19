//go:build windows

package hostgw

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/PastureStack/per-host-subnet/internal/metadata"
	"github.com/PastureStack/per-host-subnet/setting"
)

const ProviderName = "host-gateway"

type HostGw struct {
	client   metadata.Client
	interval time.Duration
	routes   windowsRouteOperations
	owned    map[string]windowsRoute
}

func New(client metadata.Client, interval time.Duration) *HostGw {
	return &HostGw{client: client, interval: interval, routes: newPowerShellRouteOperations(), owned: map[string]windowsRoute{}}
}

func (updater *HostGw) Start(ctx context.Context) {
	go func() {
		if err := updater.client.Watch(ctx, updater.interval, updater.Reload); err != nil && ctx.Err() == nil {
			slog.Error("host gateway route watcher stopped", "error", err)
		}
	}()
}

func (updater *HostGw) Reload(ctx context.Context) error {
	selfHost, err := updater.client.SelfHost(ctx)
	if err != nil {
		return fmt.Errorf("read self host metadata: %w", err)
	}
	hosts, err := updater.client.Hosts(ctx)
	if err != nil {
		return fmt.Errorf("read host metadata: %w", err)
	}
	if _, err := hostSubnetWindows(selfHost); err != nil {
		return err
	}
	routerIP, err := hostRouterIP(selfHost)
	if err != nil {
		return err
	}
	interfaces, err := interfacesWithAddress(routerIP)
	if err != nil {
		return err
	}
	if len(interfaces) != 1 {
		return fmt.Errorf("router IP must match exactly one local interface; matched %d", len(interfaces))
	}
	current, err := updater.routes.List(ctx, uint32(interfaces[0].Index))
	if err != nil {
		return fmt.Errorf("read managed routes: %w", err)
	}
	desired, err := desiredWindowsRoutes(selfHost, hosts, uint32(interfaces[0].Index))
	if err != nil {
		return fmt.Errorf("build desired routes: %w", err)
	}
	if err := reconcileWindowsRoutes(ctx, updater.routes, current, desired, updater.owned); err != nil {
		return fmt.Errorf("update managed routes: %w", err)
	}
	return nil
}

func hostRouterIP(host metadata.Host) (net.IP, error) {
	address := net.ParseIP(host.Labels[setting.RouterIPLabel])
	if address == nil || address.To4() == nil {
		return nil, fmt.Errorf("host %q has an invalid router IP label", host.UUID)
	}
	return address.To4(), nil
}

func hostSubnetWindows(host metadata.Host) (*net.IPNet, error) {
	_, subnet, err := net.ParseCIDR(host.Labels[setting.PerHostSubnetLabel])
	if err != nil || subnet.IP.To4() == nil {
		return nil, fmt.Errorf("host %q has an invalid IPv4 per-host subnet", host.UUID)
	}
	return subnet, nil
}

func interfacesWithAddress(target net.IP) ([]net.Interface, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var matches []net.Interface
	for _, candidate := range interfaces {
		addresses, err := candidate.Addrs()
		if err != nil {
			return nil, err
		}
		for _, raw := range addresses {
			address, _, err := net.ParseCIDR(raw.String())
			if err == nil && address.Equal(target) {
				matches = append(matches, candidate)
				break
			}
		}
	}
	return matches, nil
}
