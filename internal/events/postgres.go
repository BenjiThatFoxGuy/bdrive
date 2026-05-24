package events

import (
	"context"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/go-faster/errors"
	"github.com/jackc/pgx/v5"
	"github.com/tgdrive/teldrive/pkg/dto"
	"github.com/tgdrive/teldrive/pkg/repositories"
	"go.uber.org/zap"
)

type PostgresBroadcaster struct {
	*baseBroadcaster
	listenDSN    string
	pollInterval time.Duration
	lastPollSeq  atomic.Int64
}

func NewPostgresBroadcaster(ctx context.Context, eventsRepo repositories.EventRepository, listenDSN string, pollInterval time.Duration, config BroadcasterConfig, logger *zap.Logger) *PostgresBroadcaster {
	if pollInterval <= 0 {
		pollInterval = defaultEventPollInterval
	}

	ctx, cancel := context.WithCancel(ctx)
	b := &PostgresBroadcaster{
		baseBroadcaster: newBaseBroadcaster(eventsRepo, logger, ctx, cancel, config),
		listenDSN:       listenDSN,
		pollInterval:    pollInterval,
	}

	if seq, err := eventsRepo.MaxSeq(ctx); err == nil {
		b.lastPollSeq.Store(seq)
	} else {
		logger.Warn("events.max_seq_failed", zap.Error(err))
	}

	b.wg.Add(1)
	go b.pollLoop()

	if listenDSN != "" {
		b.wg.Add(1)
		go b.listenLoop()
	} else {
		logger.Warn("events.listen_disabled", zap.String("reason", "empty database DSN"))
	}

	logger.Info("events.postgres_broadcaster_created", zap.Duration("poll_interval", pollInterval))
	return b
}

func (b *PostgresBroadcaster) Record(eventType EventType, userID int64, source *dto.Source) {
	evt := createEvent(eventType, userID, source)
	model, err := eventToModel(evt)
	if err != nil {
		b.logger.Error("events.model_mapping_failed", zap.Error(err))
		return
	}

	seq, err := b.eventsRepo.CreateReturningSeq(b.ctx, model)
	if err != nil {
		b.logger.Error("events.db_save_failed",
			zap.Error(err),
			zap.String("id", evt.ID),
			zap.String("type", evt.Type),
			zap.Int64("user_id", evt.UserID))
		return
	}

	evt.Seq = seq
	if b.shouldProcess(evt.ID) {
		b.broadcast(evt)
	}
}

func (b *PostgresBroadcaster) Replay(ctx context.Context, userID int64, afterSeq int64, eventTypes []EventType, limit int) ([]dto.Event, error) {
	if limit <= 0 {
		limit = defaultEventReplayLimit
	}

	types := make([]string, 0, len(eventTypes))
	for _, eventType := range eventTypes {
		types = append(types, string(eventType))
	}

	items, err := b.eventsRepo.GetAfterSeqForUser(ctx, userID, afterSeq, types, limit)
	if err != nil {
		return nil, err
	}

	out := make([]dto.Event, 0, len(items))
	for _, item := range items {
		out = append(out, eventFromStreamItem(item))
	}
	return out, nil
}

func (b *PostgresBroadcaster) Shutdown() {
	b.logger.Info("events.postgres_broadcaster_shutting_down")
	b.cancel()

	done := make(chan struct{})
	go func() {
		b.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		b.logger.Warn("events.shutdown_timeout")
	}

	b.logger.Info("events.postgres_broadcaster_shutdown_complete")
}

func (b *PostgresBroadcaster) listenLoop() {
	defer b.wg.Done()

	for {
		select {
		case <-b.ctx.Done():
			return
		default:
		}

		conn, err := pgx.Connect(b.ctx, b.listenDSN)
		if err != nil {
			b.logger.Warn("events.listen_connect_failed", zap.Error(err))
			if !sleepOrDone(b.ctx, defaultListenerRetryDelay) {
				return
			}
			continue
		}

		if _, err := conn.Exec(b.ctx, "LISTEN "+eventsChannel); err != nil {
			b.logger.Warn("events.listen_failed", zap.Error(err))
			conn.Close(b.ctx)
			if !sleepOrDone(b.ctx, defaultListenerRetryDelay) {
				return
			}
			continue
		}

		b.logger.Info("events.listen_established", zap.String("channel", eventsChannel))
		for {
			notification, err := conn.WaitForNotification(b.ctx)
			if err != nil {
				conn.Close(context.Background())
				if b.ctx.Err() != nil {
					return
				}
				b.logger.Warn("events.listen_lost", zap.Error(err))
				break
			}

			seq, err := strconv.ParseInt(notification.Payload, 10, 64)
			if err != nil {
				b.logger.Warn("events.invalid_notify_payload", zap.String("payload", notification.Payload), zap.Error(err))
				continue
			}
			b.processSeq(seq)
		}

		if !sleepOrDone(b.ctx, defaultListenerRetryDelay) {
			return
		}
	}
}

func (b *PostgresBroadcaster) pollLoop() {
	defer b.wg.Done()

	ticker := time.NewTicker(b.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-b.ctx.Done():
			return
		case <-ticker.C:
			b.pollOnce()
		}
	}
}

func (b *PostgresBroadcaster) pollOnce() {
	lastSeq := b.lastPollSeq.Load()
	items, err := b.eventsRepo.GetAfterSeq(b.ctx, lastSeq, defaultEventReplayLimit)
	if err != nil {
		b.logger.Error("events.poll_query_failed", zap.Error(err), zap.Int64("last_seq", lastSeq))
		return
	}

	for _, item := range items {
		b.processItem(item)
		if item.Seq > lastSeq {
			lastSeq = item.Seq
			b.lastPollSeq.Store(lastSeq)
		}
	}
}

func (b *PostgresBroadcaster) processSeq(seq int64) {
	item, err := b.eventsRepo.GetBySeq(b.ctx, seq)
	if err != nil {
		if !errors.Is(err, repositories.ErrNotFound) {
			b.logger.Error("events.get_by_seq_failed", zap.Error(err), zap.Int64("seq", seq))
		}
		return
	}
	b.processItem(*item)
}

func (b *PostgresBroadcaster) processItem(item repositories.EventStreamItem) {
	evt := eventFromStreamItem(item)
	if !b.shouldProcess(evt.ID) {
		return
	}
	b.broadcast(evt)
}

func sleepOrDone(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}
