package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/PastureStack/per-host-subnet/hostnat"
	"github.com/PastureStack/per-host-subnet/hostports"
	"github.com/PastureStack/per-host-subnet/internal/metadata"
	"github.com/PastureStack/per-host-subnet/register"
	"github.com/PastureStack/per-host-subnet/routeupdate"
	"github.com/PastureStack/per-host-subnet/setting"
)

var buildVersion = "v0.0.0-dev"

type config struct {
	debug                  bool
	metadataURL            string
	metadataCARoot         string
	metadataStartupTimeout time.Duration
	watchInterval          time.Duration
	enableRouteUpdate      bool
	routeUpdateProvider    string
	natInterface           string
	registerService        bool
	unregisterService      bool
	showVersion            bool
}

func main() {
	configuration, err := parseConfig(os.Args[1:], os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if configuration.showVersion {
		fmt.Println(buildVersion)
		return
	}
	level := slog.LevelInfo
	if configuration.debug {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, configuration); err != nil {
		slog.Error("per-host subnet service stopped", "error", err)
		os.Exit(1)
	}
}

func run(parent context.Context, configuration config) error {
	handled, err := register.Configure(configuration.registerService, configuration.unregisterService)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}

	client, err := metadata.NewClient(configuration.metadataURL, configuration.metadataCARoot)
	if err != nil {
		return err
	}
	readyContext, cancelReady := context.WithTimeout(parent, configuration.metadataStartupTimeout)
	err = client.Ready(readyContext)
	cancelReady()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	if configuration.enableRouteUpdate {
		if _, err := routeupdate.Run(ctx, configuration.routeUpdateProvider, client, configuration.watchInterval); err != nil {
			return err
		}
	}
	if err := hostnat.Watch(ctx, client, configuration.watchInterval); err != nil {
		return err
	}
	if err := hostports.Watch(ctx, client, configuration.watchInterval, configuration.natInterface); err != nil {
		return err
	}

	select {
	case <-parent.Done():
	case <-register.StopChannel():
	}
	return nil
}

func parseConfig(arguments []string, getenv func(string) string) (config, error) {
	debug, err := environmentBool(getenv, "PLATFORM_DEBUG", false)
	if err != nil {
		return config{}, err
	}
	enableRouteUpdate, err := environmentBool(getenv, "PLATFORM_ENABLE_ROUTE_UPDATE", false)
	if err != nil {
		return config{}, err
	}
	metadataStartupTimeout, err := environmentDuration(getenv, "PLATFORM_METADATA_STARTUP_TIMEOUT", 2*time.Minute)
	if err != nil {
		return config{}, err
	}
	watchInterval, err := environmentDuration(getenv, "PLATFORM_WATCH_INTERVAL", 5*time.Second)
	if err != nil {
		return config{}, err
	}

	configuration := config{
		debug:                  debug,
		metadataURL:            environmentString(getenv, "PLATFORM_METADATA_URL", setting.DefaultMetadataURL),
		metadataCARoot:         strings.TrimSpace(getenv("PLATFORM_CA_ROOT")),
		metadataStartupTimeout: metadataStartupTimeout,
		watchInterval:          watchInterval,
		enableRouteUpdate:      enableRouteUpdate,
		routeUpdateProvider:    environmentString(getenv, "PLATFORM_ROUTE_UPDATE_PROVIDER", setting.DefaultRouteUpdateProvider),
		natInterface:           strings.TrimSpace(getenv("PLATFORM_NAT_INTERFACE")),
	}
	flags := flag.NewFlagSet("per-host-subnet", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.BoolVar(&configuration.debug, "debug", configuration.debug, "enable debug logging")
	flags.StringVar(&configuration.metadataURL, "metadata-url", configuration.metadataURL, "platform metadata base URL")
	flags.StringVar(&configuration.metadataCARoot, "metadata-ca-root", configuration.metadataCARoot, "PEM file containing an additional metadata CA root")
	flags.DurationVar(&configuration.metadataStartupTimeout, "metadata-startup-timeout", configuration.metadataStartupTimeout, "maximum metadata readiness wait")
	flags.DurationVar(&configuration.watchInterval, "watch-interval", configuration.watchInterval, "metadata watch retry and long-poll interval")
	flags.BoolVar(&configuration.enableRouteUpdate, "enable-route-update", configuration.enableRouteUpdate, "maintain managed host routes")
	flags.StringVar(&configuration.routeUpdateProvider, "route-update-provider", configuration.routeUpdateProvider, "managed route implementation")
	flags.StringVar(&configuration.natInterface, "nat-interface", configuration.natInterface, "Windows interface used for managed host-port mappings")
	flags.BoolVar(&configuration.registerService, "register-service", false, "register the Windows service")
	flags.BoolVar(&configuration.unregisterService, "unregister-service", false, "unregister the Windows service")
	flags.BoolVar(&configuration.showVersion, "version", false, "print the build version")
	if err := flags.Parse(arguments); err != nil {
		return config{}, fmt.Errorf("parse command-line options: %w", err)
	}
	if flags.NArg() != 0 {
		return config{}, fmt.Errorf("unexpected positional arguments")
	}
	if configuration.registerService && configuration.unregisterService {
		return config{}, fmt.Errorf("register-service and unregister-service cannot be used together")
	}
	if configuration.metadataStartupTimeout <= 0 {
		return config{}, fmt.Errorf("metadata-startup-timeout must be positive")
	}
	if configuration.watchInterval <= 0 {
		return config{}, fmt.Errorf("watch-interval must be positive")
	}
	configuration.metadataURL = strings.TrimSpace(configuration.metadataURL)
	configuration.metadataCARoot = strings.TrimSpace(configuration.metadataCARoot)
	configuration.routeUpdateProvider = strings.TrimSpace(configuration.routeUpdateProvider)
	configuration.natInterface = strings.TrimSpace(configuration.natInterface)
	if configuration.metadataURL == "" {
		return config{}, fmt.Errorf("metadata-url must not be empty")
	}
	if configuration.routeUpdateProvider == "" {
		return config{}, fmt.Errorf("route-update-provider must not be empty")
	}
	return configuration, nil
}

func environmentBool(getenv func(string) string, key string, fallback bool) (bool, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean", key)
	}
	return value, nil
}

func environmentDuration(getenv func(string) string, key string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", key)
	}
	return value, nil
}

func environmentString(getenv func(string) string, key, fallback string) string {
	if value := strings.TrimSpace(getenv(key)); value != "" {
		return value
	}
	return fallback
}
