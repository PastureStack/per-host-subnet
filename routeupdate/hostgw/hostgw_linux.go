//go:build !windows

package hostgw

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/PastureStack/per-host-subnet/internal/metadata"
)

const ProviderName = "host-gateway"

type HostGw struct {
	client   metadata.Client
	interval time.Duration
}

func New(client metadata.Client, interval time.Duration) *HostGw {
	return &HostGw{client: client, interval: interval}
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
	current, err := currentRouteEntries(selfHost)
	if err != nil {
		return fmt.Errorf("read managed routes: %w", err)
	}
	desired, err := desiredRouteEntries(selfHost, hosts)
	if err != nil {
		return fmt.Errorf("build desired routes: %w", err)
	}
	if err := updateRoutes(current, desired); err != nil {
		return fmt.Errorf("update managed routes: %w", err)
	}
	return nil
}
