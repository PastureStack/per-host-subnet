package metadata

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestClientReadsRequiredResources(t *testing.T) {
	mux := http.NewServeMux()
	responses := map[string]string{
		"/2016-07-29/version":    `"7"`,
		"/2016-07-29/self/host":  `{"name":"node-a","uuid":"host-a","agent_ip":"192.0.2.10","labels":{"io.pasturestack.network.per-host-subnet.subnet":"10.42.0.0/24"}}`,
		"/2016-07-29/hosts":      `[{"uuid":"host-a"},{"uuid":"host-b"}]`,
		"/2016-07-29/networks":   `[{"name":"transparent","uuid":"network-a"}]`,
		"/2016-07-29/containers": `[{"external_id":"runtime-a","host_uuid":"host-a","network_uuid":"network-a","primary_ip":"10.42.0.8","state":"running","ports":["0.0.0.0:8080:80/tcp"]}]`,
	}
	for path, body := range responses {
		path, body := path, body
		mux.HandleFunc(path, func(response http.ResponseWriter, _ *http.Request) {
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(body))
		})
	}
	server := httptest.NewServer(mux)
	defer server.Close()
	client, err := NewClient(server.URL+"/2016-07-29", "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := client.Ready(ctx); err != nil {
		t.Fatal(err)
	}
	host, err := client.SelfHost(ctx)
	if err != nil || host.UUID != "host-a" || host.AgentIP != "192.0.2.10" {
		t.Fatalf("host=%#v err=%v", host, err)
	}
	hosts, err := client.Hosts(ctx)
	if err != nil || len(hosts) != 2 {
		t.Fatalf("hosts=%#v err=%v", hosts, err)
	}
	networks, err := client.Networks(ctx)
	if err != nil || len(networks) != 1 || networks[0].UUID != "network-a" {
		t.Fatalf("networks=%#v err=%v", networks, err)
	}
	containers, err := client.Containers(ctx)
	if err != nil || len(containers) != 1 || containers[0].PrimaryIP != "10.42.0.8" {
		t.Fatalf("containers=%#v err=%v", containers, err)
	}
}

func TestWatchCallsOnVersionChange(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/version" {
			http.NotFound(response, request)
			return
		}
		_, _ = response.Write([]byte(`"1"`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- client.Watch(ctx, time.Millisecond, func(context.Context) error {
			calls.Add(1)
			cancel()
			return nil
		})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("watch did not stop")
	}
	if calls.Load() != 1 {
		t.Fatalf("callback count = %d", calls.Load())
	}
}

func TestClientRejectsUnsafeURLAndOversizedResponse(t *testing.T) {
	credentialURL := "http://user" + ":" + "pass@" + "metadata.invalid/path"
	for _, value := range []string{"metadata", "ftp://metadata/path", credentialURL, "http://metadata/path?token=value"} {
		if _, err := NewClient(value, ""); err == nil {
			t.Fatalf("expected %q to fail", value)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(`[]` + strings.Repeat(" ", maxResponseBytes)))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Hosts(context.Background()); err == nil {
		t.Fatal("expected oversized response to fail")
	}
}

func TestReadyRespectsContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		http.Error(response, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	if err := client.Ready(ctx); err == nil {
		t.Fatal("expected readiness timeout")
	}
}
