package db_test

import (
	"context"
	"testing"

	jetmodel "github.com/tgdrive/teldrive/internal/database/jet/gen/model"
	"github.com/tgdrive/teldrive/pkg/repositories"
)

func TestBotCreateToken(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	uid := int64(8201)
	s.ensureUserExists(uid)

	repo := repositories.NewJetBotRepository(s.pool)

	token1 := "bot_token_8201_a"
	token2 := "bot_token_8201_b"

	if err := repo.CreateToken(ctx, uid, token1); err != nil {
		t.Fatalf("CreateToken token1 failed: %v", err)
	}
	if err := repo.CreateToken(ctx, uid, token2); err != nil {
		t.Fatalf("CreateToken token2 failed: %v", err)
	}

	tokens, err := repo.GetTokensByUserID(ctx, uid)
	if err != nil {
		t.Fatalf("GetTokensByUserID failed: %v", err)
	}

	if len(tokens) != 2 {
		t.Fatalf("expected 2 tokens, got %d", len(tokens))
	}
}

func TestBotCreate(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	uid := int64(8202)
	s.ensureUserExists(uid)

	repo := repositories.NewJetBotRepository(s.pool)

	bot := &jetmodel.Bots{
		UserID: uid,
		Token:  "bot_token_8202",
		BotID:  820200,
	}

	if err := repo.Create(ctx, bot); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	bots, err := repo.GetByUserID(ctx, uid)
	if err != nil {
		t.Fatalf("GetByUserID failed: %v", err)
	}

	if len(bots) != 1 {
		t.Fatalf("expected 1 bot, got %d", len(bots))
	}
	if bots[0].Token != "bot_token_8202" {
		t.Errorf("Token mismatch: got %s, want bot_token_8202", bots[0].Token)
	}
	if bots[0].BotID != 820200 {
		t.Errorf("BotID mismatch: got %d, want %d", bots[0].BotID, 820200)
	}
}

func TestBotGetByUserID(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	uid := int64(8203)
	s.ensureUserExists(uid)

	repo := repositories.NewJetBotRepository(s.pool)

	bot1 := &jetmodel.Bots{
		UserID: uid,
		Token:  "bot_token_8203_a",
		BotID:  820301,
	}

	bot2 := &jetmodel.Bots{
		UserID: uid,
		Token:  "bot_token_8203_b",
		BotID:  820302,
	}

	if err := repo.Create(ctx, bot1); err != nil {
		t.Fatalf("Create bot1 failed: %v", err)
	}
	if err := repo.Create(ctx, bot2); err != nil {
		t.Fatalf("Create bot2 failed: %v", err)
	}

	bots, err := repo.GetByUserID(ctx, uid)
	if err != nil {
		t.Fatalf("GetByUserID failed: %v", err)
	}

	if len(bots) != 2 {
		t.Fatalf("expected 2 bots, got %d", len(bots))
	}
}

func TestBotDeleteByUserID(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	uid := int64(8204)
	s.ensureUserExists(uid)

	repo := repositories.NewJetBotRepository(s.pool)

	bot1 := &jetmodel.Bots{
		UserID: uid,
		Token:  "bot_token_8204_a",
		BotID:  820401,
	}

	bot2 := &jetmodel.Bots{
		UserID: uid,
		Token:  "bot_token_8204_b",
		BotID:  820402,
	}

	if err := repo.Create(ctx, bot1); err != nil {
		t.Fatalf("Create bot1 failed: %v", err)
	}
	if err := repo.Create(ctx, bot2); err != nil {
		t.Fatalf("Create bot2 failed: %v", err)
	}

	// Verify 2 bots exist before delete
	bots, err := repo.GetByUserID(ctx, uid)
	if err != nil {
		t.Fatalf("GetByUserID before delete failed: %v", err)
	}
	if len(bots) != 2 {
		t.Fatalf("expected 2 bots before delete, got %d", len(bots))
	}

	if err := repo.DeleteByUserID(ctx, uid); err != nil {
		t.Fatalf("DeleteByUserID failed: %v", err)
	}

	remaining, err := repo.GetByUserID(ctx, uid)
	if err != nil {
		t.Fatalf("GetByUserID after delete failed: %v", err)
	}

	if len(remaining) != 0 {
		t.Fatalf("expected 0 bots after delete, got %d", len(remaining))
	}
}
