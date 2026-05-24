package events_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tgdrive/teldrive/internal/database"
	jetmodel "github.com/tgdrive/teldrive/internal/database/jet/gen/model"
	"github.com/tgdrive/teldrive/internal/events"
	"github.com/tgdrive/teldrive/pkg/dto"
	"github.com/tgdrive/teldrive/pkg/repositories"
	"github.com/tgdrive/teldrive/tests/testdb"
	"go.uber.org/zap"
)

func setupEventsTest(t *testing.T) (*repositories.JetEventRepository, string) {
	t.Helper()

	pool := database.NewTestDatabase(t, true)
	testdb.Reset(t, pool)
	t.Cleanup(pool.Close)

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	require.NotEmpty(t, dsn)

	return repositories.NewJetEventRepository(pool), dsn
}

func newBroadcaster(ctx context.Context, repo repositories.EventRepository, dsn string, pollInterval time.Duration) *events.PostgresBroadcaster {
	return events.NewPostgresBroadcaster(ctx, repo, dsn, pollInterval, events.BroadcasterConfig{
		DBWorkers:        1,
		DBBufferSize:     16,
		DeduplicationTTL: 500 * time.Millisecond,
	}, zap.NewNop())
}

func receiveEvent(t *testing.T, ch <-chan dto.Event) dto.Event {
	t.Helper()
	select {
	case evt := <-ch:
		return evt
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for event")
		return dto.Event{}
	}
}

func assertNoEvent(t *testing.T, ch <-chan dto.Event, wait time.Duration) {
	t.Helper()
	select {
	case evt := <-ch:
		t.Fatalf("unexpected event: %+v", evt)
	case <-time.After(wait):
	}
}

func insertEvent(t *testing.T, repo repositories.EventRepository, userID int64, typ string) int64 {
	t.Helper()
	seq, err := repo.CreateReturningSeq(context.Background(), &jetmodel.Events{
		ID:        uuid.New(),
		Type:      typ,
		UserID:    userID,
		CreatedAt: time.Now().UTC(),
	})
	require.NoError(t, err)
	require.Greater(t, seq, int64(0))
	return seq
}

func TestPostgresBroadcasterRecordPersistsAndBroadcastsLocalSubscriber(t *testing.T) {
	repo, _ := setupEventsTest(t)
	ctx := context.Background()
	b := newBroadcaster(ctx, repo, "", time.Hour)
	defer b.Shutdown()

	ch := b.Subscribe(1001, []events.EventType{events.OpCreate})
	defer b.Unsubscribe(1001, ch)

	b.Record(events.OpCreate, 1001, &dto.Source{ID: uuid.NewString(), Type: "file", Name: "doc.txt"})

	evt := receiveEvent(t, ch)
	assert.Greater(t, evt.Seq, int64(0))
	assert.Equal(t, string(events.OpCreate), evt.Type)
	assert.Equal(t, int64(1001), evt.UserID)
	assert.Equal(t, "doc.txt", evt.Source.Name)

	stored, err := repo.GetBySeq(ctx, evt.Seq)
	require.NoError(t, err)
	assert.Equal(t, evt.ID, stored.ID.String())
	assert.Equal(t, evt.Type, stored.Type)
}

func TestPostgresBroadcasterNotifyFanoutAcrossInstances(t *testing.T) {
	repo, dsn := setupEventsTest(t)
	ctx := context.Background()

	listener := newBroadcaster(ctx, repo, dsn, time.Hour)
	defer listener.Shutdown()
	recorder := newBroadcaster(ctx, repo, dsn, time.Hour)
	defer recorder.Shutdown()

	ch := listener.Subscribe(1002, []events.EventType{events.OpUpdate})
	defer listener.Unsubscribe(1002, ch)

	// Let LISTEN establish before the write. The fallback poll is intentionally
	// too slow for this test, so receiving proves NOTIFY fanout works.
	time.Sleep(300 * time.Millisecond)
	recorder.Record(events.OpUpdate, 1002, &dto.Source{ID: uuid.NewString(), Type: "file", Name: "updated.txt"})

	evt := receiveEvent(t, ch)
	assert.Greater(t, evt.Seq, int64(0))
	assert.Equal(t, string(events.OpUpdate), evt.Type)
	assert.Equal(t, int64(1002), evt.UserID)
}

func TestPostgresBroadcasterPollingFallback(t *testing.T) {
	repo, _ := setupEventsTest(t)
	ctx := context.Background()
	b := newBroadcaster(ctx, repo, "", 50*time.Millisecond)
	defer b.Shutdown()

	ch := b.Subscribe(1003, nil)
	defer b.Unsubscribe(1003, ch)

	seq := insertEvent(t, repo, 1003, string(events.OpDelete))

	evt := receiveEvent(t, ch)
	assert.Equal(t, seq, evt.Seq)
	assert.Equal(t, string(events.OpDelete), evt.Type)
	assert.Equal(t, int64(1003), evt.UserID)
}

func TestPostgresBroadcasterReplayUsesSeqAndFilters(t *testing.T) {
	repo, _ := setupEventsTest(t)
	ctx := context.Background()
	b := newBroadcaster(ctx, repo, "", time.Hour)
	defer b.Shutdown()

	firstSeq := insertEvent(t, repo, 1004, string(events.OpCreate))
	insertEvent(t, repo, 1004, string(events.OpUploadProgress))
	lastSeq := insertEvent(t, repo, 1004, string(events.OpMove))
	insertEvent(t, repo, 2004, string(events.OpMove))

	replayed, err := b.Replay(ctx, 1004, firstSeq, []events.EventType{events.OpMove}, 100)
	require.NoError(t, err)
	require.Len(t, replayed, 1)
	assert.Equal(t, lastSeq, replayed[0].Seq)
	assert.Equal(t, string(events.OpMove), replayed[0].Type)
	assert.Equal(t, int64(1004), replayed[0].UserID)
}

func TestPostgresBroadcasterSubscriberFilters(t *testing.T) {
	repo, _ := setupEventsTest(t)
	ctx := context.Background()
	b := newBroadcaster(ctx, repo, "", time.Hour)
	defer b.Shutdown()

	ch := b.Subscribe(1005, []events.EventType{events.OpCopy})
	defer b.Unsubscribe(1005, ch)

	b.Record(events.OpDelete, 1005, nil)
	assertNoEvent(t, ch, 200*time.Millisecond)

	b.Record(events.OpCopy, 1005, nil)
	evt := receiveEvent(t, ch)
	assert.Equal(t, string(events.OpCopy), evt.Type)
}

func TestPostgresBroadcasterDeduplicatesNotifyAndPoll(t *testing.T) {
	repo, dsn := setupEventsTest(t)
	ctx := context.Background()
	b := newBroadcaster(ctx, repo, dsn, 50*time.Millisecond)
	defer b.Shutdown()

	ch := b.Subscribe(1006, nil)
	defer b.Unsubscribe(1006, ch)

	time.Sleep(300 * time.Millisecond)
	seq := insertEvent(t, repo, 1006, string(events.OpUploadProgress))

	evt := receiveEvent(t, ch)
	assert.Equal(t, seq, evt.Seq)
	assertNoEvent(t, ch, 250*time.Millisecond)
}
