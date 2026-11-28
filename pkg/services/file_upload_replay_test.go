package services

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tgdrive/teldrive/internal/api"
	"github.com/tgdrive/teldrive/internal/auth"
	"github.com/tgdrive/teldrive/internal/config"
	"github.com/tgdrive/teldrive/pkg/models"
	"github.com/tgdrive/teldrive/pkg/types"
)

// A completed session can collect stray part rows from a late retry of the
// part POST: two rows with part 1 and none with part 2. The file is already
// complete, but create validation sees a duplicate part number and returns
// 409 forever. The client then hammers create and logs an error each time.
func TestFilesCreateReconcilesCommittedSessionWithStrayParts(t *testing.T) {
	db := openUploadReplayTestDB(t)
	claims := &types.JWTClaims{}
	claims.Subject = strconv.FormatInt(uploadReplayTestUser, 10)
	ctx := auth.WithClaims(context.Background(), claims)

	now := time.Now().UTC()
	size := int64(200)
	no := false
	parent := "11111111-1111-1111-1111-111111111111"
	file := models.File{
		Name: "complete.flac", Type: "file", MimeType: "audio/flac", Size: &size,
		UserId: uploadReplayTestUser, Status: "active", Encrypted: &no, ParentId: &parent,
		CreatedAt: &now, UpdatedAt: &now,
	}
	require.NoError(t, db.Create(&file).Error)

	clean := []models.Upload{
		{UploadId: "complete-session", UserId: uploadReplayTestUser, Name: "complete.flac",
			PartNo: 1, PartId: 1001, Size: 100},
		{UploadId: "complete-session", UserId: uploadReplayTestUser, Name: "complete.flac",
			PartNo: 2, PartId: 1002, Size: 100},
	}
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return recordUploadCommit(tx, uploadReplayTestUser, "complete-session", file.ID, size, clean)
	}))
	// Two late part-1 POSTs, both with a fresh Telegram message id.
	stray := []models.Upload{
		{UploadId: "complete-session", UserId: uploadReplayTestUser, Name: "complete.flac",
			PartNo: 1, PartId: 2001, Size: 100},
		{UploadId: "complete-session", UserId: uploadReplayTestUser, Name: "complete.flac",
			PartNo: 1, PartId: 2002, Size: 100},
	}
	require.NoError(t, db.Create(&stray).Error)

	a := &apiService{db: db, cnf: &config.ServerCmdConfig{}}
	req := &api.File{
		Name: "complete.flac", Type: api.FileTypeFile,
		ParentId: api.NewOptString(parent), Size: api.NewOptInt64(size),
		UploadId:  api.NewOptString("complete-session"),
		ChannelId: api.NewOptInt64(-1001234567890),
		Encrypted: api.NewOptBool(false),
	}

	got, err := a.FilesCreate(ctx, req)
	require.NoError(t, err, "a committed session must reconcile instead of 409ing")
	require.NotNil(t, got)
	assert.Equal(t, file.ID, got.ID.Value)

	var left int64
	require.NoError(t, db.Model(&models.Upload{}).
		Where("upload_id = ? AND user_id = ?", "complete-session", uploadReplayTestUser).
		Count(&left).Error)
	assert.Zero(t, left, "stray part rows must be cleared when reconciling")
}
