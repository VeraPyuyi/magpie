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
		cfg, err := dotbridge.LoadConfig()
		if err != nil || !cfg.Enabled {
			return
		}
		conn, err := dotbridge.PairedConnection()
		if err == nil {
			_, _ = conn.Status(ctx)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
