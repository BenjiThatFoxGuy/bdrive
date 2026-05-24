package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	jetmodel "github.com/tgdrive/teldrive/internal/database/jet/gen/model"
	"github.com/tgdrive/teldrive/pkg/repositories"
)

func TestShareCreateAndGetByID(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	uid := int64(3001)
	s.ensureUserExists(uid)

	active := "active"
	sz := int64(100)
	fileID := uuid.New()
	now := time.Now().UTC()

	file := &jetmodel.Files{
		ID:        fileID,
		Name:      "share-target.txt",
		Type:      "file",
		MimeType:  "text/plain",
		UserID:    uid,
		Status:    &active,
		Size:      &sz,
		Encrypted: false,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.repos.Files.Create(ctx, file); err != nil {
		t.Fatalf("create file: %v", err)
	}

	shareID := uuid.New()
	share := &jetmodel.FileShares{
		ID:        shareID,
		FileID:    fileID,
		UserID:    uid,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.repos.Shares.Create(ctx, share); err != nil {
		t.Fatalf("create share: %v", err)
	}

	got, err := s.repos.Shares.GetByID(ctx, shareID)
	if err != nil {
		t.Fatalf("get share by id: %v", err)
	}

	if got.ID != shareID {
		t.Errorf("share ID mismatch: got %v want %v", got.ID, shareID)
	}
	if got.FileID != fileID {
		t.Errorf("file ID mismatch: got %v want %v", got.FileID, fileID)
	}
	if got.UserID != uid {
		t.Errorf("user ID mismatch: got %d want %d", got.UserID, uid)
	}
	if got.Password != nil {
		t.Errorf("expected nil password, got %v", *got.Password)
	}
}

func TestShareGetByFileID(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	uid := int64(3002)
	s.ensureUserExists(uid)

	active := "active"
	sz := int64(100)
	fileID := uuid.New()
	now := time.Now().UTC()

	file := &jetmodel.Files{
		ID:        fileID,
		Name:      "multi-share-target.txt",
		Type:      "file",
		MimeType:  "text/plain",
		UserID:    uid,
		Status:    &active,
		Size:      &sz,
		Encrypted: false,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.repos.Files.Create(ctx, file); err != nil {
		t.Fatalf("create file: %v", err)
	}

	share1 := &jetmodel.FileShares{
		ID:        uuid.New(),
		FileID:    fileID,
		UserID:    uid,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.repos.Shares.Create(ctx, share1); err != nil {
		t.Fatalf("create share 1: %v", err)
	}

	share2 := &jetmodel.FileShares{
		ID:        uuid.New(),
		FileID:    fileID,
		UserID:    uid,
		CreatedAt: now.Add(time.Second),
		UpdatedAt: now.Add(time.Second),
	}
	if err := s.repos.Shares.Create(ctx, share2); err != nil {
		t.Fatalf("create share 2: %v", err)
	}

	shares, err := s.repos.Shares.GetByFileID(ctx, fileID)
	if err != nil {
		t.Fatalf("get shares by file id: %v", err)
	}

	if len(shares) != 2 {
		t.Fatalf("expected 2 shares, got %d", len(shares))
	}

	ids := map[uuid.UUID]bool{share1.ID: true, share2.ID: true}
	for _, sh := range shares {
		if !ids[sh.ID] {
			t.Errorf("unexpected share id %v", sh.ID)
		}
	}
}

func TestShareNotFound(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()

	_, err := s.repos.Shares.GetByID(ctx, uuid.New())
	if err != repositories.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestShareUpdatePassword(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	uid := int64(3003)
	s.ensureUserExists(uid)

	active := "active"
	sz := int64(100)
	fileID := uuid.New()
	now := time.Now().UTC()

	file := &jetmodel.Files{
		ID:        fileID,
		Name:      "password-share-target.txt",
		Type:      "file",
		MimeType:  "text/plain",
		UserID:    uid,
		Status:    &active,
		Size:      &sz,
		Encrypted: false,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.repos.Files.Create(ctx, file); err != nil {
		t.Fatalf("create file: %v", err)
	}

	shareID := uuid.New()
	share := &jetmodel.FileShares{
		ID:        shareID,
		FileID:    fileID,
		UserID:    uid,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.repos.Shares.Create(ctx, share); err != nil {
		t.Fatalf("create share: %v", err)
	}

	passwordHash := "$2a$10$abcdefg"
	if err := s.repos.Shares.Update(ctx, shareID, repositories.ShareUpdate{
		Password: &passwordHash,
	}); err != nil {
		t.Fatalf("update share password: %v", err)
	}

	got, err := s.repos.Shares.GetByID(ctx, shareID)
	if err != nil {
		t.Fatalf("get share after update: %v", err)
	}

	if got.Password == nil {
		t.Fatal("expected password to be set, got nil")
	}
	if *got.Password != passwordHash {
		t.Errorf("password hash mismatch: got %q want %q", *got.Password, passwordHash)
	}
}

func TestShareDelete(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	uid := int64(3004)
	s.ensureUserExists(uid)

	active := "active"
	sz := int64(100)
	fileID := uuid.New()
	now := time.Now().UTC()

	file := &jetmodel.Files{
		ID:        fileID,
		Name:      "delete-share-target.txt",
		Type:      "file",
		MimeType:  "text/plain",
		UserID:    uid,
		Status:    &active,
		Size:      &sz,
		Encrypted: false,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.repos.Files.Create(ctx, file); err != nil {
		t.Fatalf("create file: %v", err)
	}

	shareID := uuid.New()
	share := &jetmodel.FileShares{
		ID:        shareID,
		FileID:    fileID,
		UserID:    uid,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.repos.Shares.Create(ctx, share); err != nil {
		t.Fatalf("create share: %v", err)
	}

	if err := s.repos.Shares.Delete(ctx, shareID); err != nil {
		t.Fatalf("delete share: %v", err)
	}

	_, err := s.repos.Shares.GetByID(ctx, shareID)
	if err != repositories.ErrNotFound {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}
