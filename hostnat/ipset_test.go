package hostnat

import (
	"reflect"
	"testing"

	"github.com/PastureStack/per-host-subnet/internal/metadata"
	"github.com/PastureStack/per-host-subnet/setting"
)

func TestIPSetXMLAndDiff(t *testing.T) {
	decoded, err := unmarshalIPSetByXML([]byte(`<ipsets><ipset><members><elem>10.42.1.0/24</elem><elem>10.42.2.0/24</elem></members></ipset></ipsets>`))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded.Members, []string{"10.42.1.0/24", "10.42.2.0/24"}) {
		t.Fatalf("members = %#v", decoded.Members)
	}
	add, remove := diffIPSetEntries(
		map[string]bool{"10.42.1.0/24": true, "10.42.2.0/24": true},
		map[string]bool{"10.42.2.0/24": true, "10.42.3.0/24": true},
	)
	if !reflect.DeepEqual(add, []string{"10.42.3.0/24"}) || !reflect.DeepEqual(remove, []string{"10.42.1.0/24"}) {
		t.Fatalf("add=%v remove=%v", add, remove)
	}
}

func TestDesiredIPSetEntriesValidatesLabels(t *testing.T) {
	self := metadata.Host{UUID: "self"}
	hosts := []metadata.Host{
		self,
		{UUID: "remote-a", Labels: map[string]string{setting.PerHostSubnetLabel: "10.42.2.18/24"}},
	}
	desired, err := desiredIPSetEntries(self, hosts)
	if err != nil {
		t.Fatal(err)
	}
	if !desired["10.42.2.0/24"] || len(desired) != 1 {
		t.Fatalf("desired = %#v", desired)
	}
	hosts[1].Labels[setting.PerHostSubnetLabel] = "invalid"
	if _, err := desiredIPSetEntries(self, hosts); err == nil {
		t.Fatal("expected invalid subnet label to fail")
	}
}
