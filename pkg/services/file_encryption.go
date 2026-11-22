package services

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	"github.com/go-faster/errors"
	"github.com/tgdrive/teldrive/internal/api"
	"github.com/tgdrive/teldrive/internal/auth"
	"github.com/tgdrive/teldrive/internal/cache"
	"github.com/tgdrive/teldrive/internal/crypt"
	"github.com/tgdrive/teldrive/internal/logging"
	"github.com/tgdrive/teldrive/internal/reader"
	"github.com/tgdrive/teldrive/internal/tgc"
	"github.com/tgdrive/teldrive/pkg/models"
	"go.uber.org/zap"
	"gorm.io/datatypes"
)

// transformFile encrypts or decrypts a file by reading its plaintext, optionally
// re-encrypting, and re-uploading every part. The DB record is updated in place
// so the file id, sharing links and placement in the tree stay the same.
func (a *apiService) transformFile(ctx context.Context, fileID string, encrypt bool) error {
	userId := auth.GetUser(ctx)
	if userId == 0 {
		return errors.New("unauthenticated")
	}
	if encrypt && a.cnf.TG.Uploads.EncryptionKey == "" {
		return errors.New("encryption is not configured")
	}

	logger := logging.Component("FILE").With(zap.String("file_id", fileID))

	// Load the file and verify ownership.
	var file models.File
	if err := a.db.WithContext(ctx).Where("id = ? AND user_id = ?", fileID, userId).First(&file).Error; err != nil {
		return err
	}
	if file.Type == "folder" || file.Size == nil || *file.Size == 0 {
		return errors.New("cannot transform an empty file or folder")
	}
	if file.IsEncrypted() == encrypt {
		return errors.New("file is already in the requested state")
	}
	if err := file.ValidateStream(); err != nil {
		return err
	}

	// Get a client and the old parts.
	client, token, _, channelUser, err := a.getUploadClient(ctx, userId)
	if err != nil {
		return err
	}
	oldParts, err := getParts(ctx, client, a.cache, &file)
	if err != nil {
		return err
	}

	// Determine the plaintext size of each part.
	// For an encrypted file, DecryptedSize holds it; for plaintext, Size does.
	partSizes := make([]int64, len(oldParts))
	var totalPlain int64
	for i, p := range oldParts {
		size := p.Size
		if file.IsEncrypted() {
			size = p.DecryptedSize
		}
		if size <= 0 {
			return errors.New("invalid part size")
		}
		partSizes[i] = size
		totalPlain += size
	}

	// Read the plaintext as one stream, then slice it back into parts.
	plainReader, err := reader.NewReader(ctx, client.API(), a.cache, &file, oldParts, 0, totalPlain-1, &a.cnf.TG, channelUser)
	if err != nil {
		return err
	}
	defer plainReader.Close()

	// Upload with channel fallback: original first, default on any failure.
	preferredChannel := *file.ChannelId
	newParts := make([]api.Part, 0, len(partSizes))
	newChannel := preferredChannel
	var oldPartIDs []int

	err = tgc.RunWithAuth(ctx, client, token, func(ctx context.Context) error {
		for i, size := range partSizes {
			buf := make([]byte, size)
			if _, err := io.ReadFull(plainReader, buf); err != nil {
				return fmt.Errorf("read part %d: %w", i, err)
			}

			partData := buf
			partSize := size
			var salt string
			if encrypt {
				salt, err = generateRandomSalt()
				if err != nil {
					return err
				}
				cipher, cipherErr := crypt.NewCipher(a.cnf.TG.Uploads.EncryptionKey, salt)
				if cipherErr != nil {
					return cipherErr
				}
				enc, encErr := cipher.EncryptData(bytes.NewReader(buf))
				if encErr != nil {
					return encErr
				}
				partData, encErr = io.ReadAll(enc)
				if encErr != nil {
					return encErr
				}
				partSize = crypt.EncryptedSize(size)
			}

			channelID, msgID, upErr := a.uploadWithChannelFallback(
				ctx, client, userId, newChannel, partData, partSize,
				fmt.Sprintf("%s.part%d", file.Name, i), a.cnf.TG.Uploads.Threads,
			)
			if upErr != nil {
				return fmt.Errorf("upload part %d: %w", i, upErr)
			}
			newChannel = channelID

			part := api.Part{ID: int(msgID)}
			if salt != "" {
				part.SetSalt(api.NewOptString(salt))
			}
			newParts = append(newParts, part)
			oldPartIDs = append(oldPartIDs, int(oldParts[i].ID))
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Mark the old Telegram parts for cleanup via the uploads table. The cron
	// job removes them after the configured retention.
	for i, id := range oldPartIDs {
		upload := models.Upload{
			UploadId:  fmt.Sprintf("transform:%s", fileID),
			UserId:    userId,
			Name:      file.Name,
			PartNo:    i,
			PartId:    id,
			ChannelId: *file.ChannelId,
			Size:      0,
			CreatedAt: time.Now().UTC(),
		}
		if err := a.db.WithContext(ctx).Create(&upload).Error; err != nil {
			logger.Warn("transform.cleanup_record_failed", zap.Error(err), zap.Int("part_id", id))
		}
	}

	// Update the file in one transaction.
	partsJSON := datatypes.NewJSONSlice(newParts)
	updates := map[string]any{
		"parts":              partsJSON,
		"encrypted":          encrypt,
		"channel_id":         newChannel,
		"referenced_file_id": nil, // transformed bytes are no longer a dedup copy
		"updated_at":         time.Now().UTC(),
	}
	if totalPlain != *file.Size {
		updates["size"] = totalPlain
	}
	if err := a.db.WithContext(ctx).Model(&models.File{}).
		Where("id = ? AND user_id = ?", fileID, userId).
		Updates(updates).Error; err != nil {
		return err
	}

	// Invalidate cached thumbnail so the next request regenerates from new bytes.
	a.db.WithContext(ctx).Where("file_id = ?", fileID).Delete(&models.Thumbnail{})

	// Invalidate caches so the next request reads the new bytes.
	_ = a.cache.Delete(ctx,
		cache.Key("files", fileID),
		cache.KeyFileMessages(fileID),
	)
	if a.fileCache != nil {
		a.fileCache.Delete(fileID)
		a.fileCache.Delete("thumb:" + fileID)
	}

	logger.Info("transform.complete", zap.Bool("encrypted", encrypt), zap.Int("parts", len(newParts)))
	return nil
}

// FilesTransform implements the generated handler for POST /files/{id}/transform.
func (a *apiService) FilesTransform(ctx context.Context, params api.FilesTransformParams) error {
	if err := a.transformFile(ctx, params.ID, params.Mode == api.FilesTransformModeEncrypt); err != nil {
		logging.Component("FILE").Error("transform.failed",
			zap.Error(err), zap.String("file_id", params.ID))
		return err
	}
	return nil
}
