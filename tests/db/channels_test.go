package db_test

import (
	"context"
	"testing"
	"time"

	jetmodel "github.com/tgdrive/teldrive/internal/database/jet/gen/model"
	"github.com/tgdrive/teldrive/pkg/repositories"
)

func TestChannelCreateAndGetByUserID(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	uid := int64(8101)
	s.ensureUserExists(uid)

	repo := repositories.NewJetChannelRepository(s.pool)
	now := time.Now().UTC()

	ch1 := &jetmodel.Channels{
		ChannelID:   810101,
		ChannelName: "channel_a",
		UserID:      uid,
		CreatedAt:   now,
	}

	ch2 := &jetmodel.Channels{
		ChannelID:   810102,
		ChannelName: "channel_b",
		UserID:      uid,
		CreatedAt:   now.Add(1 * time.Second),
	}

	if err := repo.Create(ctx, ch1); err != nil {
		t.Fatalf("Create ch1 failed: %v", err)
	}
	if err := repo.Create(ctx, ch2); err != nil {
		t.Fatalf("Create ch2 failed: %v", err)
	}

	channels, err := repo.GetByUserID(ctx, uid)
	if err != nil {
		t.Fatalf("GetByUserID failed: %v", err)
	}

	if len(channels) != 2 {
		t.Fatalf("expected 2 channels, got %d", len(channels))
	}
}

func TestChannelGetByChannelID(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	uid := int64(8102)
	s.ensureUserExists(uid)

	repo := repositories.NewJetChannelRepository(s.pool)
	now := time.Now().UTC()

	ch := &jetmodel.Channels{
		ChannelID:   810201,
		ChannelName: "test_channel",
		UserID:      uid,
		CreatedAt:   now,
	}

	if err := repo.Create(ctx, ch); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	got, err := repo.GetByChannelID(ctx, 810201)
	if err != nil {
		t.Fatalf("GetByChannelID failed: %v", err)
	}

	if got.ChannelID != 810201 {
		t.Errorf("ChannelID mismatch: got %d, want %d", got.ChannelID, 810201)
	}
	if got.ChannelName != "test_channel" {
		t.Errorf("ChannelName mismatch: got %s, want test_channel", got.ChannelName)
	}
	if got.UserID != uid {
		t.Errorf("UserID mismatch: got %d, want %d", got.UserID, uid)
	}
}

func TestChannelGetByChannelID_NotFound(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()

	repo := repositories.NewJetChannelRepository(s.pool)

	_, err := repo.GetByChannelID(ctx, 999999)
	if err != repositories.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestChannelGetSelected(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	uid := int64(8104)
	s.ensureUserExists(uid)

	repo := repositories.NewJetChannelRepository(s.pool)
	now := time.Now().UTC()
	trueVal := true
	falseVal := false

	ch1 := &jetmodel.Channels{
		ChannelID:   810401,
		ChannelName: "selected_channel",
		UserID:      uid,
		Selected:    &trueVal,
		CreatedAt:   now,
	}

	ch2 := &jetmodel.Channels{
		ChannelID:   810402,
		ChannelName: "not_selected",
		UserID:      uid,
		Selected:    &falseVal,
		CreatedAt:   now.Add(1 * time.Second),
	}

	if err := repo.Create(ctx, ch1); err != nil {
		t.Fatalf("Create ch1 failed: %v", err)
	}
	if err := repo.Create(ctx, ch2); err != nil {
		t.Fatalf("Create ch2 failed: %v", err)
	}

	got, err := repo.GetSelected(ctx, uid)
	if err != nil {
		t.Fatalf("GetSelected failed: %v", err)
	}

	if got.ChannelID != 810401 {
		t.Errorf("expected selected channel 810401, got %d", got.ChannelID)
	}
	if got.Selected == nil || !*got.Selected {
		t.Errorf("expected Selected=true, got %v", got.Selected)
	}
}

func TestChannelGetSelected_NotFound(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	uid := int64(8105)
	s.ensureUserExists(uid)

	repo := repositories.NewJetChannelRepository(s.pool)
	now := time.Now().UTC()
	falseVal := false

	ch := &jetmodel.Channels{
		ChannelID:   810501,
		ChannelName: "not_selected",
		UserID:      uid,
		Selected:    &falseVal,
		CreatedAt:   now,
	}

	if err := repo.Create(ctx, ch); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	_, err := repo.GetSelected(ctx, uid)
	if err != repositories.ErrNotFound {
		t.Fatalf("expected ErrNotFound for user with no selected channel, got %v", err)
	}
}

func TestChannelUpdate(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	uid := int64(8106)
	s.ensureUserExists(uid)

	repo := repositories.NewJetChannelRepository(s.pool)
	now := time.Now().UTC()

	ch := &jetmodel.Channels{
		ChannelID:   810601,
		ChannelName: "old_name",
		UserID:      uid,
		CreatedAt:   now,
	}

	if err := repo.Create(ctx, ch); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	newName := "new_name"
	trueVal := true
	if err := repo.Update(ctx, 810601, repositories.ChannelUpdate{
		ChannelName: &newName,
		Selected:    &trueVal,
	}); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	got, err := repo.GetByChannelID(ctx, 810601)
	if err != nil {
		t.Fatalf("GetByChannelID after update failed: %v", err)
	}

	if got.ChannelName != "new_name" {
		t.Errorf("ChannelName not updated: got %s, want new_name", got.ChannelName)
	}
	if got.Selected == nil || !*got.Selected {
		t.Errorf("Selected not updated: got %v, want true", got.Selected)
	}
}

func TestChannelDelete(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	uid := int64(8107)
	s.ensureUserExists(uid)

	repo := repositories.NewJetChannelRepository(s.pool)
	now := time.Now().UTC()

	ch := &jetmodel.Channels{
		ChannelID:   810701,
		ChannelName: "delete_me",
		UserID:      uid,
		CreatedAt:   now,
	}

	if err := repo.Create(ctx, ch); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if _, err := repo.GetByChannelID(ctx, 810701); err != nil {
		t.Fatalf("GetByChannelID before delete failed: %v", err)
	}

	if err := repo.Delete(ctx, 810701); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	_, err := repo.GetByChannelID(ctx, 810701)
	if err != repositories.ErrNotFound {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestChannelGetByUserIDCreatedAfter(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	uid := int64(8108)
	s.ensureUserExists(uid)

	repo := repositories.NewJetChannelRepository(s.pool)
	now := time.Now().UTC()

	ch1 := &jetmodel.Channels{
		ChannelID:   810801,
		ChannelName: "older_channel",
		UserID:      uid,
		CreatedAt:   now,
	}

	ch2 := &jetmodel.Channels{
		ChannelID:   810802,
		ChannelName: "newer_channel",
		UserID:      uid,
		CreatedAt:   now.Add(1 * time.Second),
	}

	if err := repo.Create(ctx, ch1); err != nil {
		t.Fatalf("Create ch1 failed: %v", err)
	}
	if err := repo.Create(ctx, ch2); err != nil {
		t.Fatalf("Create ch2 failed: %v", err)
	}

	// Fetch with after time in between the two creates
	afterTime := now.Add(500 * time.Millisecond)
	got, err := repo.GetByUserIDCreatedAfter(ctx, uid, afterTime)
	if err != nil {
		t.Fatalf("GetByUserIDCreatedAfter failed: %v", err)
	}

	if got == nil {
		t.Fatal("expected a channel, got nil")
	}
	if got.ChannelID != 810802 {
		t.Errorf("expected channel 810802 (newer), got %d", got.ChannelID)
	}
}
