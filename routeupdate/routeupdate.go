package routeupdate

import (
	"context"
	"fmt"
	"time"

	"github.com/PastureStack/per-host-subnet/internal/metadata"
	"github.com/PastureStack/per-host-subnet/routeupdate/hostgw"
)

type RouteUpdate interface {
	Start(context.Context)
	Reload(context.Context) error
}

func Run(ctx context.Context, provider string, client metadata.Client, interval time.Duration) (RouteUpdate, error) {
	switch provider {
	case hostgw.ProviderName:
		updater := hostgw.New(client, interval)
		updater.Start(ctx)
		return updater, nil
	default:
		return nil, fmt.Errorf("unsupported route update provider %q", provider)
	}
}
