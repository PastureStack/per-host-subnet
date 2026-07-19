//go:build windows

package hostports

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/PastureStack/per-host-subnet/internal/metadata"
	"github.com/PastureStack/per-host-subnet/setting"
)

type natDriver interface {
	List(context.Context) ([]PortMapping, error)
	Create(context.Context, []PortMapping) error
	Delete(context.Context, []PortMapping) error
}

type netshDriver struct {
	adapterName string
	run         func(context.Context, string, ...string) ([]byte, error)
}

func Watch(ctx context.Context, client metadata.Client, interval time.Duration, configuredInterface string) error {
	adapter, err := natInterfaceName(ctx, client, configuredInterface)
	if err != nil {
		return err
	}
	driver := &netshDriver{
		adapterName: adapter,
		run: func(ctx context.Context, name string, arguments ...string) ([]byte, error) {
			return exec.CommandContext(ctx, name, arguments...).CombinedOutput()
		},
	}
	watcher := &watcher{client: client, driver: driver, owned: map[string]PortMapping{}}
	go func() {
		if err := client.Watch(ctx, interval, watcher.onChange); err != nil && ctx.Err() == nil {
			slog.Error("Windows host-port watcher stopped", "error", err)
		}
	}()
	return nil
}

type watcher struct {
	client metadata.Client
	driver natDriver
	owned  map[string]PortMapping
}

func (watcher *watcher) onChange(ctx context.Context) error {
	host, err := watcher.client.SelfHost(ctx)
	if err != nil {
		return err
	}
	networks, err := watcher.client.Networks(ctx)
	if err != nil {
		return err
	}
	containers, err := watcher.client.Containers(ctx)
	if err != nil {
		return err
	}
	desired, err := desiredPortMappings(host, networks, containers)
	if err != nil {
		return err
	}
	return watcher.reconcile(ctx, desired)
}

func (watcher *watcher) reconcile(ctx context.Context, desired map[string]PortMapping) error {
	currentList, err := watcher.driver.List(ctx)
	if err != nil {
		return fmt.Errorf("list NAT port mappings: %w", err)
	}
	current := map[string]PortMapping{}
	for _, mapping := range currentList {
		current[mapping.Key()] = mapping
	}
	var create, remove []PortMapping
	for key, mapping := range desired {
		if existing, ok := current[key]; ok {
			if !existing.Equal(mapping) {
				return fmt.Errorf("NAT endpoint %s is owned by a different mapping", key)
			}
			continue
		}
		create = append(create, mapping)
	}
	for key, mapping := range watcher.owned {
		if _, keep := desired[key]; !keep {
			remove = append(remove, mapping)
		}
	}
	if err := watcher.driver.Delete(ctx, remove); err != nil {
		return fmt.Errorf("delete managed NAT mappings: %w", err)
	}
	if err := watcher.driver.Create(ctx, create); err != nil {
		return fmt.Errorf("create managed NAT mappings: %w", err)
	}
	for _, mapping := range remove {
		delete(watcher.owned, mapping.Key())
	}
	for _, mapping := range create {
		watcher.owned[mapping.Key()] = mapping
	}
	slog.Info("Windows host-port mappings reconciled", "desired", len(desired), "owned", len(watcher.owned), "created", len(create), "deleted", len(remove))
	return nil
}

func (driver *netshDriver) List(ctx context.Context) ([]PortMapping, error) {
	output, err := driver.run(ctx, "netsh", "routing", "ip", "nat", "show", "interface", driver.adapterName)
	if err != nil {
		return nil, fmt.Errorf("netsh list failed: %w (%s)", err, output)
	}
	return parseNetshMappings(output)
}

func (driver *netshDriver) Create(ctx context.Context, mappings []PortMapping) error {
	var operationErrors []error
	for _, mapping := range mappings {
		arguments := []string{"routing", "ip", "nat", "add", "portmapping", driver.adapterName, mapping.Protocol,
			mapping.ExternalIP.String(), strconv.Itoa(int(mapping.ExternalPort)), mapping.InternalIP.String(), strconv.Itoa(int(mapping.InternalPort))}
		if output, err := driver.run(ctx, "netsh", arguments...); err != nil {
			operationErrors = append(operationErrors, fmt.Errorf("add %s on adapter %q: %w (%s)", mapping.Key(), driver.adapterName, err, output))
		}
	}
	return errors.Join(operationErrors...)
}

func (driver *netshDriver) Delete(ctx context.Context, mappings []PortMapping) error {
	var operationErrors []error
	for _, mapping := range mappings {
		arguments := []string{"routing", "ip", "nat", "delete", "portmapping", driver.adapterName, mapping.Protocol,
			mapping.ExternalIP.String(), strconv.Itoa(int(mapping.ExternalPort))}
		if output, err := driver.run(ctx, "netsh", arguments...); err != nil {
			operationErrors = append(operationErrors, fmt.Errorf("delete %s on adapter %q: %w (%s)", mapping.Key(), driver.adapterName, err, output))
		}
	}
	return errors.Join(operationErrors...)
}

func parseNetshMappings(output []byte) ([]PortMapping, error) {
	scanner := bufio.NewScanner(bytes.NewReader(bytes.ReplaceAll(output, []byte("\r"), nil)))
	values := make([]string, 0, 5)
	var mappings []PortMapping
	flush := func() error {
		if len(values) == 0 {
			return nil
		}
		if len(values) != 5 {
			return fmt.Errorf("unexpected netsh port mapping block")
		}
		mapping, err := parsePortMapping(values[1], values[3], values[1]+":"+values[2]+":"+values[4]+"/"+strings.ToLower(values[0]))
		if err != nil {
			return err
		}
		mappings = append(mappings, mapping)
		values = values[:0]
		return nil
	}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			if err := flush(); err != nil {
				return nil, err
			}
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		value := strings.TrimSpace(parts[1])
		if len(values) == 0 && !strings.EqualFold(value, "tcp") && !strings.EqualFold(value, "udp") {
			continue
		}
		values = append(values, value)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return mappings, nil
}

func natInterfaceName(ctx context.Context, client metadata.Client, configured string) (string, error) {
	configured = strings.TrimSpace(configured)
	if configured == "" {
		return "", fmt.Errorf("a Windows NAT interface must be set with --nat-interface or PLATFORM_NAT_INTERFACE")
	}
	host, err := client.SelfHost(ctx)
	if err != nil {
		return "", err
	}
	routerIP := net.ParseIP(host.Labels[setting.RouterIPLabel])
	if routerIP == nil || routerIP.To4() == nil {
		return "", fmt.Errorf("self host metadata is missing a valid router IP label")
	}
	if skipNATInterface(configured) {
		return "", fmt.Errorf("configured NAT interface %q is not eligible", configured)
	}
	candidate, err := net.InterfaceByName(configured)
	if err != nil {
		return "", fmt.Errorf("find configured NAT interface %q: %w", configured, err)
	}
	addresses, err := candidate.Addrs()
	if err != nil {
		return "", fmt.Errorf("read configured NAT interface %q: %w", configured, err)
	}
	for _, address := range addresses {
		parsed, _, err := net.ParseCIDR(address.String())
		if err == nil && parsed.Equal(routerIP) {
			return "", fmt.Errorf("configured NAT interface %q contains the platform router IP", configured)
		}
	}
	return candidate.Name, nil
}

func skipNATInterface(name string) bool {
	lower := strings.ToLower(name)
	for _, marker := range []string{"vethernet", "isatap", "loopback"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
