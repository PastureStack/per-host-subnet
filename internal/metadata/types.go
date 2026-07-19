package metadata

type Host struct {
	Name    string            `json:"name"`
	UUID    string            `json:"uuid"`
	AgentIP string            `json:"agent_ip"`
	Labels  map[string]string `json:"labels"`
}

type Network struct {
	Name string `json:"name"`
	UUID string `json:"uuid"`
}

type Container struct {
	ExternalID  string   `json:"external_id"`
	HostUUID    string   `json:"host_uuid"`
	NetworkUUID string   `json:"network_uuid"`
	PrimaryIP   string   `json:"primary_ip"`
	State       string   `json:"state"`
	Ports       []string `json:"ports"`
}
