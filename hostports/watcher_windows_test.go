//go:build windows

package hostports

import (
	"context"
	"testing"
)

type fakeNATDriver struct {
	current []PortMapping
	created []PortMapping
	deleted []PortMapping
}

func (driver *fakeNATDriver) List(context.Context) ([]PortMapping, error) { return driver.current, nil }
func (driver *fakeNATDriver) Create(_ context.Context, mappings []PortMapping) error {
	driver.created = append(driver.created, mappings...)
	return nil
}
func (driver *fakeNATDriver) Delete(_ context.Context, mappings []PortMapping) error {
	driver.deleted = append(driver.deleted, mappings...)
	return nil
}

func TestReconcileAdoptsExactMappingsAndDeletesOnlyManaged(t *testing.T) {
	existing, _ := parsePortMapping("192.0.2.10", "10.42.0.8", "192.0.2.10:8080:80/tcp")
	added, _ := parsePortMapping("192.0.2.10", "10.42.0.9", "192.0.2.10:8081:81/tcp")
	driver := &fakeNATDriver{current: []PortMapping{existing}}
	watcher := &watcher{driver: driver, owned: map[string]PortMapping{}}
	desired := map[string]PortMapping{existing.Key(): existing, added.Key(): added}
	if err := watcher.reconcile(context.Background(), desired); err != nil {
		t.Fatal(err)
	}
	if len(driver.created) != 1 || !driver.created[0].Equal(added) || len(driver.deleted) != 0 {
		t.Fatalf("created=%v deleted=%v", driver.created, driver.deleted)
	}
	if len(watcher.owned) != 1 || !watcher.owned[added.Key()].Equal(added) {
		t.Fatalf("only newly created mappings should be owned: %#v", watcher.owned)
	}
	driver.current = []PortMapping{existing, added}
	if err := watcher.reconcile(context.Background(), map[string]PortMapping{existing.Key(): existing}); err != nil {
		t.Fatal(err)
	}
	if len(driver.deleted) != 1 || !driver.deleted[0].Equal(added) {
		t.Fatalf("deleted=%v", driver.deleted)
	}
}

func TestReconcileRejectsConflictingExistingMapping(t *testing.T) {
	desired, _ := parsePortMapping("192.0.2.10", "10.42.0.8", "192.0.2.10:8080:80/tcp")
	conflict, _ := parsePortMapping("192.0.2.10", "10.42.0.9", "192.0.2.10:8080:81/tcp")
	watcher := &watcher{driver: &fakeNATDriver{current: []PortMapping{conflict}}, owned: map[string]PortMapping{}}
	if err := watcher.reconcile(context.Background(), map[string]PortMapping{desired.Key(): desired}); err == nil {
		t.Fatal("expected mapping conflict")
	}
}

func TestParseNetshMappings(t *testing.T) {
	output := []byte("Protocol : TCP\r\nPublic IP : 192.0.2.10\r\nPublic Port : 8080\r\nPrivate IP : 10.42.0.8\r\nPrivate Port : 80\r\n\r\n")
	mappings, err := parseNetshMappings(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(mappings) != 1 || mappings[0].Key() != "tcp/192.0.2.10/8080" {
		t.Fatalf("mappings = %#v", mappings)
	}
}
