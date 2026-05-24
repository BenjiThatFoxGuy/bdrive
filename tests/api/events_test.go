package api_test

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tgdrive/teldrive/internal/events"
	"github.com/tgdrive/teldrive/pkg/dto"
)

func TestEventsAndVersionRoutes_Basic(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	_, client, _ := loginWithClient(t, s, 7207, "user7207")

	if _, err := client.VersionVersion(ctx); err != nil {
		t.Fatalf("VersionVersion failed: %v", err)
	}
	if _, err := client.EventsGetEvents(ctx); err != nil {
		t.Fatalf("EventsGetEvents failed: %v", err)
	}
}

func TestEventsStreamReplaysFromLastEventID(t *testing.T) {
	s := newHarness(t)
	userID := int64(7208)
	token := loginAndGetToken(t, s, userID, "user7208")
	eventID := uuid.NewString()
	s.events.replay = []dto.Event{{
		Seq:       42,
		ID:        eventID,
		Type:      string(events.OpCreate),
		UserID:    userID,
		Source:    &dto.Source{ID: uuid.NewString(), Type: "file", Name: "replayed.txt"},
		CreatedAt: time.Now().UTC(),
	}}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.server.URL+"/events/stream?types=files.*&interval=60000", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Last-Event-Id", "41")

	resp, err := s.httpCli.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 200, got %d: %s", resp.StatusCode, body)
	}
	require.Equal(t, http.StatusOK, resp.StatusCode)

	reader := bufio.NewReader(resp.Body)
	lines := make([]string, 0, 6)
	for len(lines) < 6 {
		line, readErr := reader.ReadString('\n')
		require.NoError(t, readErr)
		lines = append(lines, strings.TrimSpace(line))
	}
	joined := strings.Join(lines, "\n")

	assert.Equal(t, userID, s.events.replayUserID)
	assert.Equal(t, int64(41), s.events.replayAfterSeq)
	assert.Contains(t, s.events.replayEventTypes, events.OpCreate)
	assert.Contains(t, joined, "id: 42")
	assert.Contains(t, joined, "event: files.created")
	assert.Contains(t, joined, `"seq":42`)
	assert.Contains(t, joined, eventID)
}
