package db_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/google/uuid"
	jetmodel "github.com/tgdrive/teldrive/internal/database/jet/gen/model"
	"github.com/tgdrive/teldrive/pkg/repositories"
)

func TestAPIKeyCreateAndList(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	uid := int64(8010)

	s.ensureUserExists(uid)

	now := time.Now().UTC()

	tokenValue1 := "tdk_test-key-for-testing-1"
	hash1 := sha256.Sum256([]byte(tokenValue1))
	tokenHash1 := hex.EncodeToString(hash1[:])

	key1 := &jetmodel.APIKeys{
		ID:        uuid.New(),
		UserID:    uid,
		Name:      "test-key-1",
		TokenHash: tokenHash1,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.repos.APIKeys.Create(ctx, key1); err != nil {
		t.Fatalf("Create key1: %v", err)
	}

	tokenValue2 := "tdk_test-key-for-testing-2"
	hash2 := sha256.Sum256([]byte(tokenValue2))
	tokenHash2 := hex.EncodeToString(hash2[:])

	key2 := &jetmodel.APIKeys{
		ID:        uuid.New(),
		UserID:    uid,
		Name:      "test-key-2",
		TokenHash: tokenHash2,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.repos.APIKeys.Create(ctx, key2); err != nil {
		t.Fatalf("Create key2: %v", err)
	}

	keys, err := s.repos.APIKeys.ListByUserID(ctx, uid)
	if err != nil {
		t.Fatalf("ListByUserID: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(keys))
	}
}

func TestAPIKeyCreateAndGetActive(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	uid := int64(8011)

	s.ensureUserExists(uid)

	now := time.Now().UTC()
	tokenValue := "tdk_test-key-for-testing"
	hash := sha256.Sum256([]byte(tokenValue))
	tokenHash := hex.EncodeToString(hash[:])

	createdKey := &jetmodel.APIKeys{
		ID:        uuid.New(),
		UserID:    uid,
		Name:      "test-key",
		TokenHash: tokenHash,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.repos.APIKeys.Create(ctx, createdKey); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := s.repos.APIKeys.GetActiveByTokenHash(ctx, tokenHash, now)
	if err != nil {
		t.Fatalf("GetActiveByTokenHash: %v", err)
	}
	if got.ID != createdKey.ID {
		t.Fatalf("expected key ID %v, got %v", createdKey.ID, got.ID)
	}
}

func TestAPIKeyGetActive_WrongHash(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	uid := int64(8012)

	s.ensureUserExists(uid)

	now := time.Now().UTC()

	_, err := s.repos.APIKeys.GetActiveByTokenHash(ctx, "nonexistent-hash", now)
	if err != repositories.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestAPIKeyGetActive_Expired(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	uid := int64(8013)

	s.ensureUserExists(uid)

	now := time.Now().UTC()
	pastTime := now.Add(-1 * time.Hour)
	tokenValue := "tdk_test-key-expired"
	hash := sha256.Sum256([]byte(tokenValue))
	tokenHash := hex.EncodeToString(hash[:])

	createdKey := &jetmodel.APIKeys{
		ID:        uuid.New(),
		UserID:    uid,
		Name:      "expired-key",
		TokenHash: tokenHash,
		ExpiresAt: &pastTime,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.repos.APIKeys.Create(ctx, createdKey); err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err := s.repos.APIKeys.GetActiveByTokenHash(ctx, tokenHash, now)
	if err != repositories.ErrNotFound {
		t.Fatalf("expected ErrNotFound for expired key, got %v", err)
	}
}

func TestAPIKeyGetActive_Revoked(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	uid := int64(8014)

	s.ensureUserExists(uid)

	now := time.Now().UTC()
	tokenValue := "tdk_test-key-revoked"
	hash := sha256.Sum256([]byte(tokenValue))
	tokenHash := hex.EncodeToString(hash[:])

	createdKey := &jetmodel.APIKeys{
		ID:        uuid.New(),
		UserID:    uid,
		Name:      "revocable-key",
		TokenHash: tokenHash,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.repos.APIKeys.Create(ctx, createdKey); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := s.repos.APIKeys.Revoke(ctx, uid, createdKey.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	_, err := s.repos.APIKeys.GetActiveByTokenHash(ctx, tokenHash, now)
	if err != repositories.ErrNotFound {
		t.Fatalf("expected ErrNotFound for revoked key, got %v", err)
	}
}

func TestAPIKeyRevoke_OtherUser(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	uid := int64(8015)

	s.ensureUserExists(uid)

	now := time.Now().UTC()
	tokenValue := "tdk_test-key-other-user"
	hash := sha256.Sum256([]byte(tokenValue))
	tokenHash := hex.EncodeToString(hash[:])

	createdKey := &jetmodel.APIKeys{
		ID:        uuid.New(),
		UserID:    uid,
		Name:      "key-for-user-a",
		TokenHash: tokenHash,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.repos.APIKeys.Create(ctx, createdKey); err != nil {
		t.Fatalf("Create: %v", err)
	}

	wrongUserID := int64(9999)
	err := s.repos.APIKeys.Revoke(ctx, wrongUserID, createdKey.ID)
	if err != repositories.ErrNotFound {
		t.Fatalf("expected ErrNotFound when revoking with wrong user ID, got %v", err)
	}
}
