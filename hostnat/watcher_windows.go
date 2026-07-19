//go:build windows

package hostnat

import (
	"context"
	"time"

	"github.com/PastureStack/per-host-subnet/internal/metadata"
)

func Watch(context.Context, metadata.Client, time.Duration) error { return nil }
