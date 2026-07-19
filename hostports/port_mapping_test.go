package hostports

import (
	"testing"

	"github.com/PastureStack/per-host-subnet/internal/metadata"
	"github.com/PastureStack/per-host-subnet/setting"
)

func TestParsePortMapping(t *testing.T) {
	mapping, err := parsePortMapping("192.0.2.10", "10.42.0.8", ":8080:80/udp")
	if err != nil {
		t.Fatal(err)
	}
	if mapping.Key() != "udp/192.0.2.10/8080" || mapping.InternalIP.String() != "10.42.0.8" || mapping.InternalPort != 80 {
		t.Fatalf("mapping = %#v", mapping)
	}
	for _, value := range []string{"bad", "bad:80:80", "192.0.2.10:0:80", "192.0.2.10:80:0", "192.0.2.10:80:80/sctp"} {
		if _, err := parsePortMapping("192.0.2.10", "10.42.0.8", value); err == nil {
			t.Fatalf("expected %q to fail", value)
		}
	}
}

func TestDesiredPortMappings(t *testing.T) {
	host := metadata.Host{UUID: "host-a", AgentIP: "192.0.2.10"}
	networks := []metadata.Network{{Name: setting.DefaultNetworkName, UUID: "network-a"}}
	containers := []metadata.Container{
		{ExternalID: "runtime-a", HostUUID: "host-a", NetworkUUID: "network-a", PrimaryIP: "10.42.0.8", State: "running", Ports: []string{"0.0.0.0:8080:80/tcp"}},
		{ExternalID: "runtime-b", HostUUID: "host-b", NetworkUUID: "network-a", PrimaryIP: "10.42.1.8", State: "running", Ports: []string{"0.0.0.0:8081:80/tcp"}},
	}
	desired, err := desiredPortMappings(host, networks, containers)
	if err != nil {
		t.Fatal(err)
	}
	if len(desired) != 1 || desired["tcp/0.0.0.0/8080"].InternalIP.String() != "10.42.0.8" {
		t.Fatalf("desired = %#v", desired)
	}
	if _, err := desiredPortMappings(host, nil, containers); err == nil {
		t.Fatal("expected missing network to fail")
	}
}
