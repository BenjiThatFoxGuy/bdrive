package services

import (
	"context"
	"errors"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/tgdrive/teldrive/internal/api"
	"github.com/tgdrive/teldrive/pkg/mapper"
	"github.com/tgdrive/teldrive/pkg/models"
)

// committedUploadFile looks up the file an upload session was folded into.
// Only an active file counts: once the file is deleted the session is free to
// be reused by a genuinely new upload.
func (a *apiService) committedUploadFile(ctx context.Context, userId int64, uploadId string) (*models.File, error) {
	if uploadId == "" {
		return nil, nil
	}
	var commit models.UploadCommit
	if err := a.db.WithContext(ctx).
		Where("user_id = ? AND upload_id = ?", userId, uploadId).
		First(&commit).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	var file models.File
	if err := a.db.WithContext(ctx).
		Where("id = ? AND user_id = ? AND status = ?", commit.FileId, userId, "active").
		First(&file).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &file, nil
}

// restoreCommittedParts puts a completed session's part rows back into the
// uploads table. The part ids already point at the bytes on Telegram, so no
// upload is performed. Callers must only invoke this when the session has no
// live rows: a session with live rows is an active (possibly fresh) upload and
// must not be mixed with an earlier commit's parts.
func (a *apiService) restoreCommittedParts(ctx context.Context, userId int64, uploadId string) ([]models.Upload, error) {
	if uploadId == "" {
		return nil, nil
	}
	var commit models.UploadCommit
	if err := a.db.WithContext(ctx).
		Where("user_id = ? AND upload_id = ?", userId, uploadId).
		First(&commit).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	var active int64
	if err := a.db.WithContext(ctx).Model(&models.File{}).
		Where("id = ? AND user_id = ? AND status = ?", commit.FileId, userId, "active").
		Count(&active).Error; err != nil {
		return nil, err
	}
	if active == 0 || len(commit.Parts) == 0 {
		return nil, nil
	}

	rows := make([]models.Upload, 0, len(commit.Parts))
	for _, part := range commit.Parts {
		rows = append(rows, models.Upload{
			UploadId:    uploadId,
			UserId:      userId,
			Name:        part.Name,
			PartNo:      part.PartNo,
			PartId:      part.PartId,
			Encrypted:   part.Encrypted,
			Salt:        part.Salt,
			BlockHashes: part.BlockHashes,
			ChannelId:   part.ChannelId,
			Size:        part.Size,
			CreatedAt:   time.Now().UTC(),
		})
	}
	if err := a.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error; err != nil {
		return nil, err
	}

	var out []models.Upload
	if err := a.db.WithContext(ctx).
		Where("upload_id = ? AND user_id = ?", uploadId, userId).
		Order("part_no").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// recordUploadCommit snapshots the session's parts inside the same transaction
// that creates the file, so a commit is never recorded for a file that did not
// land. An upsert keeps the record correct when a session id is reused.
func recordUploadCommit(tx *gorm.DB, userId int64, uploadId string, fileID string, fileSize int64, uploads []models.Upload) error {
	if uploadId == "" {
		return nil
	}
	parts := make([]models.CommittedPart, 0, len(uploads))
	for _, upload := range uploads {
		parts = append(parts, models.CommittedPart{
			PartNo:      upload.PartNo,
			PartId:      upload.PartId,
			Name:        upload.Name,
			ChannelId:   upload.ChannelId,
			Size:        upload.Size,
			Encrypted:   upload.Encrypted,
			Salt:        upload.Salt,
			BlockHashes: upload.BlockHashes,
		})
	}
	commit := models.UploadCommit{
		UserId:    userId,
		UploadId:  uploadId,
		FileId:    fileID,
		FileSize:  fileSize,
		Parts:     datatypes.NewJSONSlice(parts),
		CreatedAt: time.Now().UTC(),
	}
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "upload_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"file_id", "file_size", "parts", "created_at"}),
	}).Create(&commit).Error
}

// replayUploadPart builds the response for a part that is already on Telegram.
// It is deliberately the same shape as a fresh upload response: the client
// must not be able to tell that the bytes were not sent again.
func replayUploadPart(upload models.Upload) *api.UploadPart {
	out := mapper.ToUploadOut([]models.Upload{upload})
	return &out[0]
}
