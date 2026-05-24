package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	jetmodel "github.com/tgdrive/teldrive/internal/database/jet/gen/model"
	"github.com/tgdrive/teldrive/pkg/repositories"
)

func TestSessionCreateAndGetByID(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	uid := int64(8001)
	s.ensureUserExists(uid)

	repo := repositories.NewJetSessionRepository(s.pool)
	sessionID := uuid.New()
	now := time.Now().UTC()
	tgSession := "tg_session_8001"

	session := &jetmodel.Sessions{
		ID:        sessionID,
		UserID:    uid,
		TgSession: tgSession,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := repo.Create(ctx, session); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	got, err := repo.GetByID(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}

	if got.ID != sessionID {
		t.Errorf("ID mismatch: got %v, want %v", got.ID, sessionID)
	}
	if got.UserID != uid {
		t.Errorf("UserID mismatch: got %d, want %d", got.UserID, uid)
	}
	if got.TgSession != tgSession {
		t.Errorf("TgSession mismatch: got %s, want %s", got.TgSession, tgSession)
	}
}

func TestSessionGetByID_Revoked(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	uid := int64(8002)
	s.ensureUserExists(uid)

	repo := repositories.NewJetSessionRepository(s.pool)
	sessionID := uuid.New()
	now := time.Now().UTC()

	session := &jetmodel.Sessions{
		ID:        sessionID,
		UserID:    uid,
		TgSession: "tg_session_8002",
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := repo.Create(ctx, session); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if err := repo.Revoke(ctx, sessionID); err != nil {
		t.Fatalf("Revoke failed: %v", err)
	}

	_, err := repo.GetByID(ctx, sessionID)
	if err != repositories.ErrNotFound {
		t.Fatalf("expected ErrNotFound after revoke, got %v", err)
	}
}

func TestSessionGetByRefreshTokenHash(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	uid := int64(8003)
	s.ensureUserExists(uid)

	repo := repositories.NewJetSessionRepository(s.pool)
	sessionID := uuid.New()
	now := time.Now().UTC()
	refreshHash := "refresh_hash_8003"

	session := &jetmodel.Sessions{
		ID:               sessionID,
		UserID:           uid,
		TgSession:        "tg_session_8003",
		RefreshTokenHash: &refreshHash,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	if err := repo.Create(ctx, session); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	got, err := repo.GetByRefreshTokenHash(ctx, refreshHash)
	if err != nil {
		t.Fatalf("GetByRefreshTokenHash failed: %v", err)
	}

	if got.ID != sessionID {
		t.Errorf("ID mismatch: got %v, want %v", got.ID, sessionID)
	}
	if got.UserID != uid {
		t.Errorf("UserID mismatch: got %d, want %d", got.UserID, uid)
	}
	if got.RefreshTokenHash == nil || *got.RefreshTokenHash != refreshHash {
		t.Errorf("RefreshTokenHash mismatch: got %v, want %s", got.RefreshTokenHash, refreshHash)
	}
}

func TestSessionGetByRefreshTokenHash_NotFound(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()

	repo := repositories.NewJetSessionRepository(s.pool)

	_, err := repo.GetByRefreshTokenHash(ctx, "nonexistent_hash_that_does_not_exist")
	if err != repositories.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestSessionUpdateRefreshTokenHash(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	uid := int64(8005)
	s.ensureUserExists(uid)

	repo := repositories.NewJetSessionRepository(s.pool)
	sessionID := uuid.New()
	now := time.Now().UTC()
	oldHash := "old_refresh_hash_8005"
	newHash := "new_refresh_hash_8005"

	session := &jetmodel.Sessions{
		ID:               sessionID,
		UserID:           uid,
		TgSession:        "tg_session_8005",
		RefreshTokenHash: &oldHash,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	if err := repo.Create(ctx, session); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if err := repo.UpdateRefreshTokenHash(ctx, sessionID, newHash); err != nil {
		t.Fatalf("UpdateRefreshTokenHash failed: %v", err)
	}

	// Old hash should no longer be found
	_, err := repo.GetByRefreshTokenHash(ctx, oldHash)
	if err != repositories.ErrNotFound {
		t.Fatalf("expected ErrNotFound for old hash, got %v", err)
	}

	// Session should still be found by ID with the new hash
	got, err := repo.GetByID(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetByID after update failed: %v", err)
	}
	if got.ID != sessionID {
		t.Errorf("ID mismatch: got %v, want %v", got.ID, sessionID)
	}
	if got.RefreshTokenHash == nil || *got.RefreshTokenHash != newHash {
		t.Errorf("RefreshTokenHash wasn't updated: got %v, want %s", got.RefreshTokenHash, newHash)
	}
}

func TestSessionGetByUserID(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	uid := int64(8006)
	s.ensureUserExists(uid)

	repo := repositories.NewJetSessionRepository(s.pool)
	now := time.Now().UTC()

	session1 := &jetmodel.Sessions{
		ID:        uuid.New(),
		UserID:    uid,
		TgSession: "tg_session_8006_a",
		CreatedAt: now,
		UpdatedAt: now,
	}

	session2 := &jetmodel.Sessions{
		ID:        uuid.New(),
		UserID:    uid,
		TgSession: "tg_session_8006_b",
		CreatedAt: now.Add(1 * time.Second),
		UpdatedAt: now.Add(1 * time.Second),
	}

	if err := repo.Create(ctx, session1); err != nil {
		t.Fatalf("Create session1 failed: %v", err)
	}
	if err := repo.Create(ctx, session2); err != nil {
		t.Fatalf("Create session2 failed: %v", err)
	}

	sessions, err := repo.GetByUserID(ctx, uid)
	if err != nil {
		t.Fatalf("GetByUserID failed: %v", err)
	}

	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}

	// Should be ordered by CreatedAt DESC
	if sessions[0].ID != session2.ID {
		t.Errorf("expected first session to be the newest (session2), got %v", sessions[0].ID)
	}
	if sessions[1].ID != session1.ID {
		t.Errorf("expected second session to be the oldest (session1), got %v", sessions[1].ID)
	}
}

func TestSessionRevoke(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	uid := int64(8007)
	s.ensureUserExists(uid)

	repo := repositories.NewJetSessionRepository(s.pool)
	sessionID := uuid.New()
	now := time.Now().UTC()

	session := &jetmodel.Sessions{
		ID:        sessionID,
		UserID:    uid,
		TgSession: "tg_session_8007",
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := repo.Create(ctx, session); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Verify it exists
	if _, err := repo.GetByID(ctx, sessionID); err != nil {
		t.Fatalf("GetByID before revoke failed: %v", err)
	}

	if err := repo.Revoke(ctx, sessionID); err != nil {
		t.Fatalf("Revoke failed: %v", err)
	}

	_, err := repo.GetByID(ctx, sessionID)
	if err != repositories.ErrNotFound {
		t.Fatalf("expected ErrNotFound after revoke, got %v", err)
	}
}
