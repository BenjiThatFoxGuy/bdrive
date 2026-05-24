package db_test

import (
	"context"
	"errors"
	"testing"
	"time"

	jetmodel "github.com/tgdrive/teldrive/internal/database/jet/gen/model"
	"github.com/tgdrive/teldrive/pkg/repositories"
)

func TestUserCreateAndGetByID(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	uid := int64(8105)
	s.ensureUserExists(uid)

	userRepo := repositories.NewJetUserRepository(s.pool)

	got, err := userRepo.GetByID(ctx, uid)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got.UserID != uid {
		t.Errorf("UserID mismatch: got %d, want %d", got.UserID, uid)
	}
	if got.UserName != "user_8105" {
		t.Errorf("UserName mismatch: got %s, want user_8105", got.UserName)
	}
}

func TestUserGetByID_NotFound(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()

	userRepo := repositories.NewJetUserRepository(s.pool)

	_, err := userRepo.GetByID(ctx, 99999)
	if !errors.Is(err, repositories.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestUserUpdate(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	uid := int64(8107)
	s.ensureUserExists(uid)

	userRepo := repositories.NewJetUserRepository(s.pool)

	name := "updated-name"
	isPremium := true
	err := userRepo.Update(ctx, uid, repositories.UserUpdate{
		Name:      &name,
		IsPremium: &isPremium,
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	got, err := userRepo.GetByID(ctx, uid)
	if err != nil {
		t.Fatalf("GetByID after update failed: %v", err)
	}
	if got.Name == nil || *got.Name != "updated-name" {
		t.Errorf("Name not updated: got %v, want updated-name", got.Name)
	}
	if !got.IsPremium {
		t.Errorf("IsPremium not updated: got false, want true")
	}
}

func TestUserExists(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()

	userRepo := repositories.NewJetUserRepository(s.pool)

	// Create user directly
	uid := int64(8108)
	now := time.Now().UTC()
	name := "exists-test"
	if err := userRepo.Create(ctx, &jetmodel.Users{
		UserID:    uid,
		UserName:  "exists_test_user",
		Name:      &name,
		IsPremium: false,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	exists, err := userRepo.Exists(ctx, uid)
	if err != nil {
		t.Fatalf("Exists for existing user failed: %v", err)
	}
	if !exists {
		t.Errorf("expected Exists to return true for existing user")
	}

	notExists, err := userRepo.Exists(ctx, 99999)
	if err != nil {
		t.Fatalf("Exists for non-existing user failed: %v", err)
	}
	if notExists {
		t.Errorf("expected Exists to return false for non-existing user")
	}
}

func TestUserCreate_ConflictDoesNotError(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	uid := int64(8109)

	userRepo := repositories.NewJetUserRepository(s.pool)

	now := time.Now().UTC()
	name := "conflict-test"
	user := &jetmodel.Users{
		UserID:    uid,
		UserName:  "conflict_test_user",
		Name:      &name,
		IsPremium: false,
		CreatedAt: now,
		UpdatedAt: now,
	}

	// First create should succeed
	if err := userRepo.Create(ctx, user); err != nil {
		t.Fatalf("first Create failed: %v", err)
	}

	// Second create with same UserID should NOT error (ON CONFLICT DO NOTHING)
	if err := userRepo.Create(ctx, user); err != nil {
		t.Fatalf("second Create with same UserID should not error, got: %v", err)
	}
}
