package setting

const (
	DefaultMetadataURL         = "http://metadata/2016-07-29"
	DefaultRouteUpdateProvider = "host-gateway"
	DefaultHostNATIPSet        = "pasturestack-no-host-nat"
	DefaultNetworkName         = "transparent"

	PerHostSubnetLabel   = "io.pasturestack.network.per-host-subnet.subnet"
	RouterIPLabel        = "io.pasturestack.network.per-host-subnet.router-ip"
	OverrideAgentIPLabel = "io.pasturestack.network.per-host-subnet.override-agent-ip"
)
