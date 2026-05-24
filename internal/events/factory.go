package events

import (
	"context"
	"time"

	"github.com/tgdrive/teldrive/pkg/repositories"
	"go.uber.org/zap"
)

func NewBroadcaster(ctx context.Context, eventsRepo repositories.EventRepository, listenDSN string, pollInterval time.Duration, config BroadcasterConfig, logger *zap.Logger) EventBroadcaster {
	logger.Debug("events.using_postgres_broadcaster", zap.Duration("poll_interval", pollInterval))
	return NewPostgresBroadcaster(ctx, eventsRepo, listenDSN, pollInterval, config, logger)
}
