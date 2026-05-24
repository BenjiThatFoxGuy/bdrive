package db_test

import (
	"context"
	"testing"
	"time"

	jetmodel "github.com/tgdrive/teldrive/internal/database/jet/gen/model"
	"github.com/tgdrive/teldrive/pkg/repositories"
)

func TestUploadCreateAndGet(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	uid := int64(8101)
	s.ensureUserExists(uid)

	repo := repositories.NewJetUploadRepository(s.pool)

	now := time.Now().UTC()
	upload := &jetmodel.Uploads{
		UploadID:  "create-get-upload-1",
		Name:      "part-1",
		UserID:    &uid,
		PartNo:    1,
		PartID:    100,
		ChannelID: 910001,
		Size:      100,
		Encrypted: false,
		CreatedAt: &now,
	}

	if err := repo.Create(ctx, upload); err != nil {
		t.Fatalf("Create: %v", err)
	}

	results, err := repo.GetByUploadID(ctx, "create-get-upload-1")
	if err != nil {
		t.Fatalf("GetByUploadID: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	got := results[0]
	if got.UploadID != "create-get-upload-1" {
		t.Errorf("UploadID = %q, want %q", got.UploadID, "create-get-upload-1")
	}
	if got.Name != "part-1" {
		t.Errorf("Name = %q, want %q", got.Name, "part-1")
	}
	if got.PartNo != 1 {
		t.Errorf("PartNo = %d, want %d", got.PartNo, 1)
	}
	if got.PartID != 100 {
		t.Errorf("PartID = %d, want %d", got.PartID, 100)
	}
	if got.ChannelID != 910001 {
		t.Errorf("ChannelID = %d, want %d", got.ChannelID, 910001)
	}
	if got.Size != 100 {
		t.Errorf("Size = %d, want %d", got.Size, 100)
	}
	if got.Encrypted != false {
		t.Errorf("Encrypted = %v, want %v", got.Encrypted, false)
	}
	if got.UserID == nil || *got.UserID != uid {
		t.Errorf("UserID = %v, want %d", got.UserID, uid)
	}
}

func TestUploadCreateMultipleParts(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	uid := int64(8102)
	s.ensureUserExists(uid)

	repo := repositories.NewJetUploadRepository(s.pool)

	uploadID := "multi-part-upload"
	now := time.Now().UTC()

	parts := []jetmodel.Uploads{
		{
			UploadID:  uploadID,
			Name:      "part-1",
			UserID:    &uid,
			PartNo:    1,
			PartID:    101,
			ChannelID: 910001,
			Size:      100,
			Encrypted: false,
			CreatedAt: &now,
		},
		{
			UploadID:  uploadID,
			Name:      "part-2",
			UserID:    &uid,
			PartNo:    2,
			PartID:    102,
			ChannelID: 910001,
			Size:      200,
			Encrypted: false,
			CreatedAt: &now,
		},
		{
			UploadID:  uploadID,
			Name:      "part-3",
			UserID:    &uid,
			PartNo:    3,
			PartID:    103,
			ChannelID: 910001,
			Size:      300,
			Encrypted: false,
			CreatedAt: &now,
		},
	}

	for _, p := range parts {
		if err := repo.Create(ctx, &p); err != nil {
			t.Fatalf("Create part %d: %v", p.PartNo, err)
		}
	}

	results, err := repo.GetByUploadID(ctx, uploadID)
	if err != nil {
		t.Fatalf("GetByUploadID: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	for i, r := range results {
		expectedPartNo := int32(i + 1)
		if r.PartNo != expectedPartNo {
			t.Errorf("results[%d].PartNo = %d, want %d", i, r.PartNo, expectedPartNo)
		}
	}
}

func TestUploadDelete(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	uid := int64(8103)
	s.ensureUserExists(uid)

	repo := repositories.NewJetUploadRepository(s.pool)

	now := time.Now().UTC()
	upload := &jetmodel.Uploads{
		UploadID:  "delete-test-upload",
		Name:      "to-delete",
		UserID:    &uid,
		PartNo:    1,
		PartID:    200,
		ChannelID: 910001,
		Size:      50,
		Encrypted: false,
		CreatedAt: &now,
	}

	if err := repo.Create(ctx, upload); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := repo.Delete(ctx, "delete-test-upload"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	results, err := repo.GetByUploadID(ctx, "delete-test-upload")
	if err != nil {
		t.Fatalf("GetByUploadID after delete: %v", err)
	}

	if len(results) != 0 {
		t.Fatalf("expected 0 results after delete, got %d", len(results))
	}
}

func TestUploadGetByRetention(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	uid := int64(8104)
	s.ensureUserExists(uid)

	repo := repositories.NewJetUploadRepository(s.pool)

	old := time.Now().UTC().Add(-2 * time.Hour)
	upload := &jetmodel.Uploads{
		UploadID:  "retention-old-upload",
		Name:      "old-part",
		UserID:    &uid,
		PartNo:    1,
		PartID:    300,
		ChannelID: 910001,
		Size:      75,
		Encrypted: false,
		CreatedAt: &old,
	}

	if err := repo.Create(ctx, upload); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Retention of 1 hour — the upload is 2h old, so it should be excluded.
	results, err := repo.GetByUploadIDAndRetention(ctx, "retention-old-upload", time.Hour)
	if err != nil {
		t.Fatalf("GetByUploadIDAndRetention: %v", err)
	}

	if len(results) != 0 {
		t.Fatalf("expected 0 results (upload older than retention), got %d", len(results))
	}
}

func TestUploadGetByRetention_Fresh(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	uid := int64(8105)
	s.ensureUserExists(uid)

	repo := repositories.NewJetUploadRepository(s.pool)

	now := time.Now().UTC()
	upload := &jetmodel.Uploads{
		UploadID:  "retention-fresh-upload",
		Name:      "fresh-part",
		UserID:    &uid,
		PartNo:    1,
		PartID:    400,
		ChannelID: 910001,
		Size:      125,
		Encrypted: false,
		CreatedAt: &now,
	}

	if err := repo.Create(ctx, upload); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Retention of 1 hour — the upload is fresh, so it should be included.
	results, err := repo.GetByUploadIDAndRetention(ctx, "retention-fresh-upload", time.Hour)
	if err != nil {
		t.Fatalf("GetByUploadIDAndRetention: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result (upload within retention), got %d", len(results))
	}
}
