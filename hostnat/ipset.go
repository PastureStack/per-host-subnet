package hostnat

import (
	"encoding/xml"
	"fmt"
	"net"
	"sort"
	"strings"

	"github.com/PastureStack/per-host-subnet/internal/metadata"
	"github.com/PastureStack/per-host-subnet/setting"
)

type ipsets struct {
	XMLName xml.Name `xml:"ipsets"`
	Members []string `xml:"ipset>members>elem"`
}

func unmarshalIPSetByXML(data []byte) (ipsets, error) {
	result := ipsets{}
	if err := xml.Unmarshal(data, &result); err != nil {
		return result, fmt.Errorf("decode ipset XML: %w", err)
	}
	return result, nil
}

func desiredIPSetEntries(selfHost metadata.Host, allHosts []metadata.Host) (map[string]bool, error) {
	desired := map[string]bool{}
	for _, host := range allHosts {
		if host.UUID == selfHost.UUID {
			continue
		}
		raw := strings.TrimSpace(host.Labels[setting.PerHostSubnetLabel])
		_, subnet, err := net.ParseCIDR(raw)
		if err != nil || subnet.IP.To4() == nil {
			return nil, fmt.Errorf("host %q has an invalid per-host subnet", host.UUID)
		}
		desired[subnet.String()] = true
	}
	return desired, nil
}

func diffIPSetEntries(current, desired map[string]bool) (toAdd, toDelete []string) {
	for entry := range desired {
		if !current[entry] {
			toAdd = append(toAdd, entry)
		}
	}
	for entry := range current {
		if !desired[entry] {
			toDelete = append(toDelete, entry)
		}
	}
	sort.Strings(toAdd)
	sort.Strings(toDelete)
	return
}
