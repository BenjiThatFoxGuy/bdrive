package events

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/tgdrive/teldrive/internal/api"
	"github.com/tgdrive/teldrive/pkg/dto"
	"github.com/tgdrive/teldrive/pkg/repositories"
	"go.uber.org/zap"
)

type EventType = api.EventType

const (
	OpCreate         EventType = api.EventTypeFilesCreated
	OpUpdate         EventType = api.EventTypeFilesUpdated
	OpDelete         EventType = api.EventTypeFilesDeleted
	OpMove           EventType = api.EventTypeFilesMoved
	OpCopy           EventType = api.EventTypeFilesCopied
	OpUploadProgress EventType = api.EventTypeUploadsProgress
)

const (
	eventsChannel             = "teldrive_events"
	defaultDBWorkers          = 10
	defaultDBBufferSize       = 1000
	defaultDeduplicationTTL   = 30 * time.Minute
	defaultSubscriberBufSize  = 100
	defaultEventReplayLimit   = 1000
	defaultEventPollInterval  = 10 * time.Second
	defaultListenerRetryDelay = 5 * time.Second
)

type EventBroadcaster interface {
	Subscribe(userID int64, eventTypes []EventType) chan dto.Event
	Unsubscribe(userID int64, ch chan dto.Event)
	Record(eventType EventType, userID int64, source *dto.Source)
	Replay(ctx context.Context, userID int64, afterSeq int64, eventTypes []EventType, limit int) ([]dto.Event, error)
	Shutdown()
}

type eventSubscriber struct {
	ch      chan dto.Event
	filters map[EventType]struct{}
}

type BroadcasterConfig struct {
	DBWorkers        int
	DBBufferSize     int
	DeduplicationTTL time.Duration
}

func DefaultBroadcasterConfig() BroadcasterConfig {
	return BroadcasterConfig{
		DBWorkers:        defaultDBWorkers,
		DBBufferSize:     defaultDBBufferSize,
		DeduplicationTTL: defaultDeduplicationTTL,
	}
}

type baseBroadcaster struct {
	eventsRepo   repositories.EventRepository
	logger       *zap.Logger
	ctx          context.Context
	cancel       context.CancelFunc
	subscribers  map[int64][]eventSubscriber
	subMu        sync.RWMutex
	wg           sync.WaitGroup
	recentEvents map[string]time.Time
	eventMu      sync.RWMutex
	config       BroadcasterConfig
}

func newBaseBroadcaster(eventsRepo repositories.EventRepository, logger *zap.Logger, ctx context.Context, cancel context.CancelFunc, config BroadcasterConfig) *baseBroadcaster {
	if config.DBWorkers <= 0 {
		config.DBWorkers = defaultDBWorkers
	}
	if config.DBBufferSize <= 0 {
		config.DBBufferSize = defaultDBBufferSize
	}
	if config.DeduplicationTTL <= 0 {
		config.DeduplicationTTL = defaultDeduplicationTTL
	}

	b := &baseBroadcaster{
		eventsRepo:   eventsRepo,
		logger:       logger,
		ctx:          ctx,
		cancel:       cancel,
		subscribers:  make(map[int64][]eventSubscriber),
		recentEvents: make(map[string]time.Time),
		config:       config,
	}
	b.startDedupCleanup()

	return b
}

func (b *baseBroadcaster) startDedupCleanup() {
	interval := b.config.DeduplicationTTL
	if interval > time.Minute {
		interval = time.Minute
	}
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-b.ctx.Done():
				return
			case <-ticker.C:
				b.eventMu.Lock()
				b.cleanupOldEvents()
				b.eventMu.Unlock()
			}
		}
	}()
}

func (b *baseBroadcaster) shouldProcess(eventID string) bool {
	b.eventMu.Lock()
	defer b.eventMu.Unlock()

	if ts, ok := b.recentEvents[eventID]; ok {
		if time.Since(ts) < b.config.DeduplicationTTL {
			return false
		}
		delete(b.recentEvents, eventID)
	}

	b.recentEvents[eventID] = time.Now()
	return true
}

func (b *baseBroadcaster) cleanupOldEvents() {
	now := time.Now()
	for id, ts := range b.recentEvents {
		if now.Sub(ts) > b.config.DeduplicationTTL {
			delete(b.recentEvents, id)
		}
	}
}

func (b *baseBroadcaster) broadcast(evt dto.Event) {
	b.subMu.RLock()
	subs, ok := b.subscribers[evt.UserID]
	b.subMu.RUnlock()

	if !ok || len(subs) == 0 {
		return
	}

	eventType := EventType(evt.Type)
	for i, sub := range subs {
		if len(sub.filters) > 0 {
			if _, ok := sub.filters[eventType]; !ok {
				continue
			}
		}

		select {
		case sub.ch <- evt:
		default:
			b.logger.Debug("events.channel_full",
				zap.String("id", evt.ID),
				zap.Int64("seq", evt.Seq),
				zap.Int("subscriber_index", i))
		}
	}
}

func (b *baseBroadcaster) Subscribe(userID int64, eventTypes []EventType) chan dto.Event {
	ch := make(chan dto.Event, defaultSubscriberBufSize)
	filters := make(map[EventType]struct{}, len(eventTypes))
	for _, eventType := range eventTypes {
		filters[eventType] = struct{}{}
	}

	b.subMu.Lock()
	b.subscribers[userID] = append(b.subscribers[userID], eventSubscriber{ch: ch, filters: filters})
	userSubs := len(b.subscribers[userID])
	b.subMu.Unlock()

	b.logger.Debug("events.subscribed", zap.Int64("user_id", userID), zap.Int("user_subs", userSubs))

	return ch
}

func (b *baseBroadcaster) Unsubscribe(userID int64, ch chan dto.Event) {
	b.subMu.Lock()

	if subs, ok := b.subscribers[userID]; ok {
		for i, sub := range subs {
			if sub.ch == ch {
				b.subscribers[userID] = append(subs[:i], subs[i+1:]...)
				break
			}
		}
		if len(b.subscribers[userID]) == 0 {
			delete(b.subscribers, userID)
		}
	}

	b.subMu.Unlock()

	go func() {
		timeout := time.After(100 * time.Millisecond)
		for {
			select {
			case <-ch:
			case <-timeout:
				close(ch)
				return
			}
		}
	}()

	b.logger.Debug("events.unsubscribed", zap.Int64("user_id", userID))
}

func createEvent(eventType EventType, userID int64, source *dto.Source) dto.Event {
	return dto.Event{
		ID:        uuid.New().String(),
		Type:      string(eventType),
		UserID:    userID,
		Source:    source,
		CreatedAt: time.Now().UTC(),
	}
}
