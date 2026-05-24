package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	jetmodel "github.com/tgdrive/teldrive/internal/database/jet/gen/model"
	"github.com/tgdrive/teldrive/pkg/repositories"
)

func TestEventCreateAndGetRecent(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	uid := int64(8101)
	s.ensureUserExists(uid)

	eventRepo := repositories.NewJetEventRepository(s.pool)

	now := time.Now().UTC()

	e1 := &jetmodel.Events{
		ID:        uuid.New(),
		Type:      "login",
		UserID:    uid,
		CreatedAt: now.Add(-5 * time.Minute),
	}
	if err := eventRepo.Create(ctx, e1); err != nil {
		t.Fatalf("Create event 1 failed: %v", err)
	}

	e2 := &jetmodel.Events{
		ID:        uuid.New(),
		Type:      "logout",
		UserID:    uid,
		CreatedAt: now.Add(-1 * time.Minute),
	}
	if err := eventRepo.Create(ctx, e2); err != nil {
		t.Fatalf("Create event 2 failed: %v", err)
	}

	events, err := eventRepo.GetRecent(ctx, uid, now.Add(-10*time.Minute), 100)
	if err != nil {
		t.Fatalf("GetRecent failed: %v", err)
	}

	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
}

func TestEventGetRecent_WithLimit(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	uid := int64(8102)
	s.ensureUserExists(uid)

	eventRepo := repositories.NewJetEventRepository(s.pool)

	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		e := &jetmodel.Events{
			ID:        uuid.New(),
			Type:      "test",
			UserID:    uid,
			CreatedAt: now.Add(time.Duration(i) * time.Minute),
		}
		if err := eventRepo.Create(ctx, e); err != nil {
			t.Fatalf("Create event %d failed: %v", i, err)
		}
	}

	events, err := eventRepo.GetRecent(ctx, uid, now.Add(-10*time.Minute), 1)
	if err != nil {
		t.Fatalf("GetRecent failed: %v", err)
	}

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
}

func TestEventGetSince(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	uid := int64(8103)
	s.ensureUserExists(uid)

	eventRepo := repositories.NewJetEventRepository(s.pool)

	now := time.Now().UTC()

	// Create event before the cutoff
	old := &jetmodel.Events{
		ID:        uuid.New(),
		Type:      "test-get-since-old",
		UserID:    uid,
		CreatedAt: now.Add(-10 * time.Second),
	}
	if err := eventRepo.Create(ctx, old); err != nil {
		t.Fatalf("create old event: %v", err)
	}

	// Create event after the cutoff
	current := &jetmodel.Events{
		ID:        uuid.New(),
		Type:      "test-get-since-current",
		UserID:    uid,
		CreatedAt: now,
	}
	if err := eventRepo.Create(ctx, current); err != nil {
		t.Fatalf("create current event: %v", err)
	}

	// GetSince with cutoff in the middle
	since := now.Add(-5 * time.Second)
	events, err := eventRepo.GetSince(ctx, since, 100)
	if err != nil {
		t.Fatalf("GetSince failed: %v", err)
	}

	foundOld := false
	foundCurrent := false
	for _, e := range events {
		switch e.Type {
		case "test-get-since-old":
			foundOld = true
		case "test-get-since-current":
			foundCurrent = true
		}
	}
	if foundOld {
		t.Errorf("old event should not be returned by GetSince")
	}
	if !foundCurrent {
		t.Errorf("current event should be returned by GetSince")
	}
}

func TestEventDeleteOlderThan(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	uid := int64(8104)
	s.ensureUserExists(uid)

	eventRepo := repositories.NewJetEventRepository(s.pool)

	now := time.Now().UTC()

	// Older event
	old := &jetmodel.Events{
		ID:        uuid.New(),
		Type:      "old",
		UserID:    uid,
		CreatedAt: now.Add(-10 * time.Minute),
	}
	if err := eventRepo.Create(ctx, old); err != nil {
		t.Fatalf("create old event: %v", err)
	}

	// Newer event
	newEv := &jetmodel.Events{
		ID:        uuid.New(),
		Type:      "new",
		UserID:    uid,
		CreatedAt: now,
	}
	if err := eventRepo.Create(ctx, newEv); err != nil {
		t.Fatalf("create new event: %v", err)
	}

	// Delete older than midpoint
	deleted, err := eventRepo.DeleteOlderThan(ctx, now.Add(-5*time.Minute))
	if err != nil {
		t.Fatalf("DeleteOlderThan failed: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected 1 deleted row, got %d", deleted)
	}

	// Verify only newer event remains
	events, err := eventRepo.GetRecent(ctx, uid, now.Add(-30*time.Minute), 100)
	if err != nil {
		t.Fatalf("GetRecent failed: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != "new" {
		t.Fatalf("expected 'new' event, got '%s'", events[0].Type)
	}
}
