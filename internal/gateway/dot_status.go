package gateway

import (
	"context"
	"time"

	"github.com/yetone/magpie/internal/dotbridge"
)

func init() {
	WhileServing = append(WhileServing, refreshDotStatus)
}

func refreshDotStatus(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		// A disabled or unreadable boot config may be enabled or repaired
		// while serving. Keep polling until the server itself stops.
		if cfg, err := dotbridge.LoadConfig(); err == nil && cfg.Enabled {
			if conn, err := dotbridge.PairedConnection(); err == nil {
				_, _ = conn.Status(ctx)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
