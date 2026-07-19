package main

import (
	"testing"
	"time"
)

func TestParseConfigUsesNeutralEnvironment(t *testing.T) {
	values := map[string]string{
		"PLATFORM_DEBUG":                    "true",
		"PLATFORM_METADATA_URL":             "https://metadata.example.test/v1",
		"PLATFORM_CA_ROOT":                  "ca.pem",
		"PLATFORM_METADATA_STARTUP_TIMEOUT": "45s",
		"PLATFORM_WATCH_INTERVAL":           "7s",
		"PLATFORM_ENABLE_ROUTE_UPDATE":      "true",
		"PLATFORM_ROUTE_UPDATE_PROVIDER":    "host-gateway",
		"PLATFORM_NAT_INTERFACE":            "Ethernet 2",
	}
	configuration, err := parseConfig(nil, func(key string) string { return values[key] })
	if err != nil {
		t.Fatal(err)
	}
	if !configuration.debug || !configuration.enableRouteUpdate {
		t.Fatalf("boolean environment values were not applied: %+v", configuration)
	}
	if configuration.metadataURL != values["PLATFORM_METADATA_URL"] || configuration.metadataCARoot != "ca.pem" {
		t.Fatalf("metadata environment values were not applied: %+v", configuration)
	}
	if configuration.metadataStartupTimeout != 45*time.Second || configuration.watchInterval != 7*time.Second {
		t.Fatalf("duration environment values were not applied: %+v", configuration)
	}
	if configuration.natInterface != "Ethernet 2" {
		t.Fatalf("NAT interface environment value was not applied: %+v", configuration)
	}
}

func TestParseConfigFlagsOverrideEnvironment(t *testing.T) {
	configuration, err := parseConfig([]string{
		"--metadata-url", "http://127.0.0.1:8080/v2",
		"--watch-interval", "3s",
		"--enable-route-update=false",
	}, func(key string) string {
		values := map[string]string{
			"PLATFORM_METADATA_URL":        "http://metadata/v1",
			"PLATFORM_WATCH_INTERVAL":      "9s",
			"PLATFORM_ENABLE_ROUTE_UPDATE": "true",
		}
		return values[key]
	})
	if err != nil {
		t.Fatal(err)
	}
	if configuration.metadataURL != "http://127.0.0.1:8080/v2" || configuration.watchInterval != 3*time.Second || configuration.enableRouteUpdate {
		t.Fatalf("flags did not override environment: %+v", configuration)
	}
}

func TestParseConfigRejectsUnsafeAmbiguity(t *testing.T) {
	tests := [][]string{
		{"--register-service", "--unregister-service"},
		{"--watch-interval", "0s"},
		{"--metadata-startup-timeout", "-1s"},
		{"unexpected"},
	}
	for _, arguments := range tests {
		if _, err := parseConfig(arguments, func(string) string { return "" }); err == nil {
			t.Fatalf("expected arguments %v to fail", arguments)
		}
	}
}

func TestParseConfigRejectsInvalidEnvironment(t *testing.T) {
	tests := map[string]string{
		"PLATFORM_DEBUG":                    "sometimes",
		"PLATFORM_METADATA_STARTUP_TIMEOUT": "soon",
		"PLATFORM_WATCH_INTERVAL":           "0s",
	}
	for key, value := range tests {
		if _, err := parseConfig(nil, func(candidate string) string {
			if candidate == key {
				return value
			}
			return ""
		}); err == nil {
			t.Fatalf("expected %s=%q to fail", key, value)
		}
	}
}
