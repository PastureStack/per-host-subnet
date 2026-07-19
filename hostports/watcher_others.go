//go:build !windows

package hostports

import (
	"context"
	"time"

	"github.com/PastureStack/per-host-subnet/internal/metadata"
)

func Watch(context.Context, metadata.Client, time.Duration, string) error { return nil }
