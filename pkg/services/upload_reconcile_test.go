package services

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"

	"github.com/tgdrive/teldrive/internal/api"
	"github.com/tgdrive/teldrive/internal/auth"
	"github.com/tgdrive/teldrive/internal/config"
	"github.com/tgdrive/teldrive/pkg/models"
	"github.com/tgdrive/teldrive/pkg/types"
)

const uploadReplayTestUser = int64(987654322)

// openUploadReplayTestDB follows the same skip-if-unset pattern as the other
// DB-backed service tests, so `go test ./pkg/services` still works locally.
func openUploadReplayTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TELDRIVE_DB_DATASOURCE")
	if dsn == "" {
		t.Skip("TELDRIVE_DB_DATASOURCE not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{TablePrefix: "teldrive.", SingularTable: false},
		Logger:         logger.Default.LogMode(logger.Silent),
		NowFunc:        func() time.Time { return time.Now().UTC() },
	})
	require.NoError(t, err)

	// The full migration chain needs pgroonga, which a plain test Postgres
	// does not have. Build only the tables these tests touch, then apply the
	// real migration file so its SQL is exercised too.
	for _, ddl := range []string{
		`CREATE SCHEMA IF NOT EXISTS teldrive`,
		`DROP TABLE IF EXISTS teldrive.upload_commits`,
		`DROP TABLE IF EXISTS teldrive.files`,
		`DROP TABLE IF EXISTS teldrive.uploads`,
		`CREATE TABLE IF NOT EXISTS teldrive.files (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			name text NOT NULL, type text NOT NULL, mime_type text NOT NULL,
			size bigint, user_id bigint NOT NULL, status text, parent_id uuid,
			parts jsonb, channel_id bigint, hash text, encrypted boolean DEFAULT false,
			starred boolean DEFAULT false, category text, referenced_file_id uuid,
			created_at timestamp, updated_at timestamp
		)`,
		`CREATE TABLE IF NOT EXISTS teldrive.uploads (
			upload_id text, name text, user_id bigint, part_no int,
			part_id int PRIMARY KEY, channel_id bigint, size bigint,
			encrypted boolean DEFAULT false, salt text, block_hashes bytea,
			created_at timestamp
		)`,
	} {
		require.NoError(t, db.Exec(ddl).Error)
	}
	migration, err := os.ReadFile("../../internal/database/migrations/20261122120000_add_upload_commits.sql")
	require.NoError(t, err)
	// goose runs only the Up section; the file also contains a Down section
	// that would drop the table again if executed as one string.
	up := string(migration)
	if i := strings.Index(up, "-- +goose Down"); i >= 0 {
		up = up[:i]
	}
	require.NoError(t, db.Exec(up).Error)

	require.NoError(t, db.Where("user_id = ?", uploadReplayTestUser).Delete(&models.UploadCommit{}).Error)
	require.NoError(t, db.Where("user_id = ?", uploadReplayTestUser).Delete(&models.Upload{}).Error)
	require.NoError(t, db.Where("user_id = ?", uploadReplayTestUser).Delete(&models.File{}).Error)
	return db
}

func TestReplayUploadPart(t *testing.T) {
	part := models.Upload{
		Name: "song.flac", PartId: 42, PartNo: 1, ChannelId: -1001234567890,
		Size: 1234, Encrypted: true, Salt: "c2FsdA==",
	}
	got := replayUploadPart(part)
	require.NotNil(t, got)
	assert.Equal(t, "song.flac", got.Name)
	assert.Equal(t, 42, got.PartId)
	assert.Equal(t, 1, got.PartNo)
	assert.Equal(t, int64(-1001234567890), got.ChannelId)
	assert.Equal(t, int64(1234), got.Size)
	assert.True(t, got.Encrypted)
	assert.True(t, got.Salt.Set)
	assert.Equal(t, "c2FsdA==", got.Salt.Value)
}

func TestUploadCommitReplay(t *testing.T) {
	db := openUploadReplayTestDB(t)
	ctx := context.Background()
	a := &apiService{db: db}

	now := time.Now().UTC()
	size := int64(100)
	no := false
	file := models.File{
		Name: "song.flac", Type: "file", MimeType: "audio/flac", Size: &size,
		UserId: uploadReplayTestUser, Status: "active", Encrypted: &no,
		CreatedAt: &now, UpdatedAt: &now,
	}
	require.NoError(t, db.Create(&file).Error)

	uploads := []models.Upload{{
		UploadId: "replay-session", UserId: uploadReplayTestUser, Name: "song.flac",
		PartNo: 1, PartId: 777, ChannelId: -1001234567890, Size: size,
		BlockHashes: []byte{1, 2, 3},
	}}
	require.NoError(t, db.Create(&uploads).Error)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return recordUploadCommit(tx, uploadReplayTestUser, "replay-session", file.ID, size, uploads)
	}))
	// The create handler deletes live rows after a commit.
	require.NoError(t, db.Where("upload_id = ? AND user_id = ?", "replay-session", uploadReplayTestUser).
		Delete(&models.Upload{}).Error)

	committed, err := a.committedUploadFile(ctx, uploadReplayTestUser, "replay-session")
	require.NoError(t, err)
	require.NotNil(t, committed)
	assert.Equal(t, file.ID, committed.ID)

	restored, err := a.restoreCommittedParts(ctx, uploadReplayTestUser, "replay-session")
	require.NoError(t, err)
	require.Len(t, restored, 1)
	assert.Equal(t, 777, restored[0].PartId)
	assert.Equal(t, int64(-1001234567890), restored[0].ChannelId)
	assert.Equal(t, []byte{1, 2, 3}, restored[0].BlockHashes)

	// Replaying again must not insert a second row or a second Telegram ref.
	restored, err = a.restoreCommittedParts(ctx, uploadReplayTestUser, "replay-session")
	require.NoError(t, err)
	require.Len(t, restored, 1)

	// A deleted file frees the session id; no stale parts may be restored.
	require.NoError(t, db.Delete(&file).Error)
	committed, err = a.committedUploadFile(ctx, uploadReplayTestUser, "replay-session")
	require.NoError(t, err)
	assert.Nil(t, committed)
	restored, err = a.restoreCommittedParts(ctx, uploadReplayTestUser, "replay-session")
	require.NoError(t, err)
	assert.Empty(t, restored)
}

func TestUploadCommitUpsert(t *testing.T) {
	db := openUploadReplayTestDB(t)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return recordUploadCommit(tx, uploadReplayTestUser, "reused-session", "00000000-0000-0000-0000-000000000001", 10, nil)
	}))
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return recordUploadCommit(tx, uploadReplayTestUser, "reused-session", "00000000-0000-0000-0000-000000000002", 20, nil)
	}))

	var commits []models.UploadCommit
	require.NoError(t, db.Where("user_id = ? AND upload_id = ?", uploadReplayTestUser, "reused-session").Find(&commits).Error)
	require.Len(t, commits, 1)
	assert.Equal(t, "00000000-0000-0000-0000-000000000002", commits[0].FileId)
	assert.Equal(t, int64(20), commits[0].FileSize)
}

func TestUploadPartsAreContiguous(t *testing.T) {
	row := func(no int) models.Upload { return models.Upload{PartNo: no, PartId: no} }
	for _, tt := range []struct {
		name   string
		parts  []models.Upload
		contig bool
	}{
		{"single part", []models.Upload{row(1)}, true},
		{"two parts", []models.Upload{row(1), row(2)}, true},
		{"duplicate part one", []models.Upload{row(1), row(1)}, false},
		{"gap", []models.Upload{row(1), row(3)}, false},
		{"empty", nil, true},
		{"zero part number", []models.Upload{row(0)}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.contig, uploadPartsAreContiguous(tt.parts))
		})
	}
}

func TestUploadUploadRetiresConflictingPartRows(t *testing.T) {
	db := openUploadReplayTestDB(t)
	claims := &types.JWTClaims{}
	claims.Subject = strconv.FormatInt(uploadReplayTestUser, 10)
	ctx := auth.WithClaims(context.Background(), claims)

	stale := models.Upload{
		UploadId: "stale-session", UserId: uploadReplayTestUser, Name: "song.flac",
		PartNo: 1, PartId: 999, Size: 100,
	}
	require.NoError(t, db.Create(&stale).Error)

	a := &apiService{db: db, cnf: &config.ServerCmdConfig{}}
	require.NoError(t, a.retireStaleUploadSession(ctx, uploadReplayTestUser, "stale-session"))

	var left int64
	require.NoError(t, db.Model(&models.Upload{}).
		Where("upload_id = ? AND user_id = ?", "stale-session", uploadReplayTestUser).
		Count(&left).Error)
	assert.Zero(t, left, "a conflicting part must retire the session, not leave a duplicate part number")
}

func TestUploadEndpointsReplayFromCommit(t *testing.T) {
	db := openUploadReplayTestDB(t)
	claims := &types.JWTClaims{}
	claims.Subject = strconv.FormatInt(uploadReplayTestUser, 10)
	ctx := auth.WithClaims(context.Background(), claims)

	now := time.Now().UTC()
	size := int64(100)
	no := false
	file := models.File{
		Name: "song.flac", Type: "file", MimeType: "audio/flac", Size: &size,
		UserId: uploadReplayTestUser, Status: "active", Encrypted: &no,
		CreatedAt: &now, UpdatedAt: &now,
	}
	require.NoError(t, db.Create(&file).Error)
	uploads := []models.Upload{{
		UploadId: "endpoint-session", UserId: uploadReplayTestUser, Name: "song.flac",
		PartNo: 1, PartId: 555, ChannelId: -1001234567890, Size: size,
	}}
	require.NoError(t, db.Create(&uploads).Error)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return recordUploadCommit(tx, uploadReplayTestUser, "endpoint-session", file.ID, size, uploads)
	}))
	require.NoError(t, db.Where("upload_id = ? AND user_id = ?", "endpoint-session", uploadReplayTestUser).
		Delete(&models.Upload{}).Error)

	// Minimal service: if the replay guard misses, these calls fall through
	// to client creation and fail instead of reaching Telegram.
	a := &apiService{db: db, cnf: &config.ServerCmdConfig{}}

	parts, err := a.UploadsPartsById(ctx, api.UploadsPartsByIdParams{ID: "endpoint-session"})
	require.NoError(t, err)
	require.Len(t, parts, 1)
	assert.Equal(t, 555, parts[0].PartId)

	part, err := a.UploadsUpload(ctx, nil, api.UploadsUploadParams{
		ID: "endpoint-session", PartNo: 1, ContentLength: size,
		PartName: "song.flac", FileName: "song.flac",
	})
	require.NoError(t, err)
	require.NotNil(t, part)
	assert.Equal(t, 555, part.PartId)
	assert.Equal(t, size, part.Size)

	var count int64
	require.NoError(t, db.Model(&models.Upload{}).
		Where("upload_id = ? AND user_id = ?", "endpoint-session", uploadReplayTestUser).
		Count(&count).Error)
	assert.Equal(t, int64(1), count, "replay must create exactly one part row")
}
