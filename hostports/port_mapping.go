package hostports

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/PastureStack/per-host-subnet/internal/metadata"
	"github.com/PastureStack/per-host-subnet/setting"
)

type PortMapping struct {
	Protocol     string
	InternalIP   net.IP
	InternalPort uint16
	ExternalIP   net.IP
	ExternalPort uint16
}

func (mapping PortMapping) Key() string {
	return strings.ToLower(mapping.Protocol) + "/" + mapping.ExternalIP.String() + "/" + strconv.Itoa(int(mapping.ExternalPort))
}

func (mapping PortMapping) Equal(other PortMapping) bool {
	return strings.EqualFold(mapping.Protocol, other.Protocol) && mapping.InternalIP.Equal(other.InternalIP) &&
		mapping.InternalPort == other.InternalPort && mapping.ExternalIP.Equal(other.ExternalIP) && mapping.ExternalPort == other.ExternalPort
}

func parsePortMapping(defaultExternalIP, internalIP, value string) (PortMapping, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 3 {
		return PortMapping{}, fmt.Errorf("port mapping must have external-ip:external-port:internal-port format")
	}
	externalAddress := strings.TrimSpace(parts[0])
	if externalAddress == "" {
		externalAddress = defaultExternalIP
	}
	externalIP := net.ParseIP(externalAddress)
	containerIP := net.ParseIP(strings.TrimSpace(internalIP))
	if externalIP == nil || externalIP.To4() == nil || containerIP == nil || containerIP.To4() == nil {
		return PortMapping{}, fmt.Errorf("port mapping requires valid IPv4 addresses")
	}
	protocol := "tcp"
	internalPortText := parts[2]
	if portParts := strings.Split(parts[2], "/"); len(portParts) == 2 {
		internalPortText = portParts[0]
		protocol = strings.ToLower(portParts[1])
	} else if len(portParts) != 1 {
		return PortMapping{}, fmt.Errorf("invalid internal port and protocol")
	}
	if protocol != "tcp" && protocol != "udp" {
		return PortMapping{}, fmt.Errorf("port mapping protocol must be tcp or udp")
	}
	externalPort, err := strconv.ParseUint(parts[1], 10, 16)
	if err != nil || externalPort == 0 {
		return PortMapping{}, fmt.Errorf("invalid external port")
	}
	internalPort, err := strconv.ParseUint(internalPortText, 10, 16)
	if err != nil || internalPort == 0 {
		return PortMapping{}, fmt.Errorf("invalid internal port")
	}
	return PortMapping{
		Protocol:     protocol,
		InternalIP:   containerIP.To4(),
		InternalPort: uint16(internalPort),
		ExternalIP:   externalIP.To4(),
		ExternalPort: uint16(externalPort),
	}, nil
}

func desiredPortMappings(host metadata.Host, networks []metadata.Network, containers []metadata.Container) (map[string]PortMapping, error) {
	networkID := ""
	for _, network := range networks {
		if network.Name == setting.DefaultNetworkName {
			networkID = network.UUID
			break
		}
	}
	if networkID == "" {
		return nil, fmt.Errorf("network %q was not found in metadata", setting.DefaultNetworkName)
	}
	desired := map[string]PortMapping{}
	for _, container := range containers {
		if container.HostUUID != host.UUID || container.NetworkUUID != networkID || !activeContainerState(container.State) {
			continue
		}
		for _, value := range container.Ports {
			mapping, err := parsePortMapping(host.AgentIP, container.PrimaryIP, value)
			if err != nil {
				return nil, fmt.Errorf("container %q port %q: %w", container.ExternalID, value, err)
			}
			key := mapping.Key()
			if existing, ok := desired[key]; ok && !existing.Equal(mapping) {
				return nil, fmt.Errorf("conflicting metadata port mapping %s", key)
			}
			desired[key] = mapping
		}
	}
	return desired, nil
}

func activeContainerState(value string) bool {
	switch strings.ToLower(value) {
	case "running", "starting", "stopping":
		return true
	default:
		return false
	}
}
