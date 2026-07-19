//go:build !windows

package hostnat

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"time"

	"github.com/PastureStack/per-host-subnet/internal/metadata"
	"github.com/PastureStack/per-host-subnet/setting"
)

type commandRunner func(context.Context, string, ...string) ([]byte, error)

func Watch(ctx context.Context, client metadata.Client, interval time.Duration) error {
	ipsetPath, err := exec.LookPath("ipset")
	if err != nil {
		return fmt.Errorf("find ipset: %w", err)
	}
	watcher := &watcher{
		client:    client,
		ipsetName: setting.DefaultHostNATIPSet,
		ipsetPath: ipsetPath,
		run: func(ctx context.Context, name string, arguments ...string) ([]byte, error) {
			return exec.CommandContext(ctx, name, arguments...).CombinedOutput()
		},
	}
	go func() {
		if err := client.Watch(ctx, interval, watcher.onChange); err != nil && ctx.Err() == nil {
			slog.Error("host NAT watcher stopped", "error", err)
		}
	}()
	return nil
}

type watcher struct {
	client    metadata.Client
	ipsetName string
	ipsetPath string
	run       commandRunner
}

func (watcher *watcher) onChange(ctx context.Context) error {
	selfHost, err := watcher.client.SelfHost(ctx)
	if err != nil {
		return err
	}
	hosts, err := watcher.client.Hosts(ctx)
	if err != nil {
		return err
	}
	return watcher.refresh(ctx, selfHost, hosts)
}

func (watcher *watcher) refresh(ctx context.Context, selfHost metadata.Host, hosts []metadata.Host) error {
	if output, err := watcher.run(ctx, watcher.ipsetPath, "create", watcher.ipsetName, "hash:net", "family", "inet", "-exist"); err != nil {
		return fmt.Errorf("create host NAT ipset: %w (%s)", err, output)
	}
	current, err := watcher.currentEntries(ctx)
	if err != nil {
		return err
	}
	desired, err := desiredIPSetEntries(selfHost, hosts)
	if err != nil {
		return err
	}
	toAdd, toDelete := diffIPSetEntries(current, desired)
	var operationErrors []error
	for _, entry := range toAdd {
		if output, err := watcher.run(ctx, watcher.ipsetPath, "add", watcher.ipsetName, entry, "-exist"); err != nil {
			operationErrors = append(operationErrors, fmt.Errorf("add ipset entry %s: %w (%s)", entry, err, output))
		}
	}
	for _, entry := range toDelete {
		if output, err := watcher.run(ctx, watcher.ipsetPath, "del", watcher.ipsetName, entry, "-exist"); err != nil {
			operationErrors = append(operationErrors, fmt.Errorf("delete ipset entry %s: %w (%s)", entry, err, output))
		}
	}
	return errors.Join(operationErrors...)
}

func (watcher *watcher) currentEntries(ctx context.Context) (map[string]bool, error) {
	output, err := watcher.run(ctx, watcher.ipsetPath, "list", "-o", "xml", watcher.ipsetName)
	if err != nil {
		return nil, fmt.Errorf("list host NAT ipset: %w (%s)", err, output)
	}
	decoded, err := unmarshalIPSetByXML(output)
	if err != nil {
		return nil, err
	}
	entries := map[string]bool{}
	for _, entry := range decoded.Members {
		entries[entry] = true
	}
	return entries, nil
}
