package services

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/tgdrive/teldrive/internal/api"
	"github.com/tgdrive/teldrive/internal/appcontext"
	"github.com/tgdrive/teldrive/internal/auth"
	"github.com/tgdrive/teldrive/internal/cache"
	"github.com/tgdrive/teldrive/internal/category"
	"github.com/tgdrive/teldrive/internal/database"
	"github.com/tgdrive/teldrive/internal/events"
	"github.com/tgdrive/teldrive/internal/hash"
	"github.com/tgdrive/teldrive/internal/http_range"
	"github.com/tgdrive/teldrive/internal/logging"
	"github.com/tgdrive/teldrive/internal/md5"
	"github.com/tgdrive/teldrive/internal/reader"
	"github.com/tgdrive/teldrive/internal/tgc"
	"github.com/tgdrive/teldrive/internal/utils"
	"github.com/tgdrive/teldrive/pkg/mapper"
	"github.com/tgdrive/teldrive/pkg/models"
	"github.com/tgdrive/teldrive/pkg/types"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrorStreamAbandoned = errors.New("stream abandoned")
	defaultContentType   = "application/octet-stream"
)

func isUUID(str string) bool {
	_, err := uuid.Parse(str)
	return err == nil
}

// FindDeduplicateFile finds an existing file with the same hash for a user
// Returns the file if found, or nil if not found
// Only considers non-encrypted files and active status
func (a *apiService) FindDeduplicateFile(ctx context.Context, userId int64, fileHash string) (*models.File, error) {
	if fileHash == "" {
		return nil, nil // No hash provided, no dedup check
	}

	var file models.File
	if err := a.db.Where(
		"user_id = ? AND hash = ? AND encrypted = false AND status = 'active'",
		userId, fileHash,
	).Order("created_at ASC"). // Get oldest matching file as canonical
					First(&file).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil // No duplicate found
		}
		return nil, err
	}
	return &file, nil
}

// GetDeduplicationReferences finds all files that share the same hash with a given file
// Used to show users what other files have the same content
func (a *apiService) GetDeduplicationReferences(ctx context.Context, fileId string) ([]models.File, error) {
	// First, get the file to find its hash
	var sourceFile models.File
	if err := a.db.Where("id = ?", fileId).First(&sourceFile).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return []models.File{}, nil
		}
		return nil, err
	}

	if sourceFile.Hash == nil || *sourceFile.Hash == "" {
		return []models.File{}, nil // No hash, no references
	}

	// Find all other files with same hash (including through ReferencedFileId)
	var references []models.File
	err := a.db.Where(
		"user_id = ? AND hash = ? AND id != ? AND encrypted = false AND status = 'active'",
		sourceFile.UserId, *sourceFile.Hash, fileId,
	).Find(&references).Error

	if err != nil {
		return nil, err
	}
	return references, nil
}

// CountDedupReferences counts how many other files share the same hash with a given file
// Returns 0 if no other files share the hash (meaning this is the only copy or first canonical copy)
func (a *apiService) CountDedupReferences(ctx context.Context, fileId string) (int64, error) {
	var sourceFile models.File
	if err := a.db.Where("id = ?", fileId).First(&sourceFile).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, nil
		}
		return 0, err
	}

	if sourceFile.Hash == nil || *sourceFile.Hash == "" {
		return 0, nil // No hash, no references
	}

	var count int64
	err := a.db.Model(&models.File{}).Where(
		"user_id = ? AND hash = ? AND id != ? AND encrypted = false AND status = 'active'",
		sourceFile.UserId, *sourceFile.Hash, fileId,
	).Count(&count).Error

	return count, err
}

func (a *apiService) FilesCategoryStats(ctx context.Context) ([]api.CategoryStats, error) {
	userId := auth.GetUser(ctx)
	var stats []api.CategoryStats
	if err := a.db.Model(&models.File{}).Select("category", "COUNT(*) as total_files", "coalesce(SUM(size),0) as total_size").
		Where(&models.File{UserId: userId, Type: "file", Status: "active"}).
		Order("category ASC").Group("category").Find(&stats).Error; err != nil {
		return nil, &apiError{err: err}
	}

	return stats, nil
}

func (a *apiService) FilesCopy(ctx context.Context, req *api.FileCopy, params api.FilesCopyParams) (*api.File, error) {
	userId := auth.GetUser(ctx)

	client, _ := tgc.AuthClient(ctx, &a.cnf.TG, auth.GetJWTUser(ctx).TgSession, a.newMiddlewares(ctx, 5)...)

	var res []models.File

	if err := a.db.Model(&models.File{}).Where("id = ?", params.ID).Find(&res).Error; err != nil {
		return nil, &apiError{err: err}
	}
	if len(res) == 0 {
		return nil, &apiError{err: errors.New("file not found"), code: 404}
	}

	file := res[0]

	newIds := []api.Part{}

	channelId, err := a.channelManager.CurrentChannel(ctx, userId)
	if err != nil {
		return nil, &apiError{err: err}
	}

	err = tgc.RunWithAuth(ctx, client, "", func(ctx context.Context) error {

		ids := utils.Map(*file.Parts, func(part api.Part) int { return part.ID })
		messages, err := tgc.GetMessages(ctx, client.API(), ids, *file.ChannelId)

		if err != nil {
			return err
		}

		channel, err := tgc.GetChannelById(ctx, client.API(), channelId)

		if err != nil {
			return err
		}
		for i, message := range messages {
			item := message.(*tg.Message)
			media := item.Media.(*tg.MessageMediaDocument)
			document := media.Document.(*tg.Document)

			id, _ := client.RandInt64()
			request := tg.MessagesSendMediaRequest{
				Silent:   true,
				Peer:     &tg.InputPeerChannel{ChannelID: channel.ChannelID, AccessHash: channel.AccessHash},
				Media:    &tg.InputMediaDocument{ID: document.AsInput()},
				RandomID: id,
			}
			res, err := client.API().MessagesSendMedia(ctx, &request)

			if err != nil {
				return err
			}

			updates := res.(*tg.Updates)

			var msg *tg.Message

			for _, update := range updates.Updates {
				channelMsg, ok := update.(*tg.UpdateNewChannelMessage)
				if ok {
					msg = channelMsg.Message.(*tg.Message)
					break
				}

			}
			p := api.Part{ID: msg.ID}
			if (*file.Parts)[i].Salt.Value != "" {
				p.Salt = (*file.Parts)[i].Salt
			}
			newIds = append(newIds, p)

		}
		return nil
	})

	if err != nil {
		return nil, &apiError{err: err}
	}

	if len(newIds) != len(*file.Parts) {
		return nil, &apiError{err: errors.New("failed to copy all file parts")}
	}

	dbFile := models.File{}

	dbFile.Name = req.NewName.Or(file.Name)
	dbFile.Size = file.Size
	dbFile.Type = file.Type
	dbFile.MimeType = file.MimeType
	if len(newIds) > 0 {
		dbFile.Parts = utils.Ptr(datatypes.NewJSONSlice(newIds))
	}
	dbFile.UserId = userId
	dbFile.Status = "active"
	dbFile.ChannelId = &channelId
	dbFile.Encrypted = file.Encrypted
	dbFile.Category = file.Category
	dbFile.Hash = file.Hash // Preserve hash during copy (content is identical)
	if req.UpdatedAt.IsSet() && !req.UpdatedAt.Value.IsZero() {
		dbFile.UpdatedAt = utils.Ptr(req.UpdatedAt.Value)
	} else {
		dbFile.UpdatedAt = utils.Ptr(time.Now().UTC())
	}

	// Use a transaction so destination-folder creation and the file copy commit
	// (or roll back) together - otherwise a failure creating dbFile can leave an
	// empty destination folder behind with nothing copied into it.
	var parentId string
	err = a.db.Transaction(func(tx *gorm.DB) error {
		if !isUUID(req.Destination) {
			var destRes []models.File
			if err := database.RetryTransientLock(ctx, database.DefaultLockRetryAttempts, func() error {
				return tx.Raw("select * from teldrive.create_directories(?, ?)", userId, req.Destination).
					Scan(&destRes).Error
			}); err != nil {
				return err
			}
			parentId = destRes[0].ID
		} else {
			parentId = req.Destination
		}
		dbFile.ParentId = utils.Ptr(parentId)

		return database.RetryTransientLock(ctx, database.DefaultLockRetryAttempts, func() error {
			return tx.Create(&dbFile).Error
		})
	})
	if err != nil {
		return nil, &apiError{err: err}
	}

	a.events.Record(events.OpCopy, userId, &models.Source{
		ID:       dbFile.ID,
		Type:     dbFile.Type,
		Name:     dbFile.Name,
		ParentID: parentId,
	})
	return mapper.ToFileOut(dbFile), nil
}

func (a *apiService) FilesCreate(ctx context.Context, fileIn *api.File) (*api.File, error) {
	userId := auth.GetUser(ctx)

	var (
		fileDB    models.File
		parentID  *string
		err       error
		path      string
		channelId int64
		uploadId  string
		uploads   []models.Upload
	)

	if fileIn.Path.Value == "" && fileIn.ParentId.Value == "" {
		return nil, &apiError{err: errors.New("parent id or path is required"), code: 409}
	}

	if fileIn.Path.Value != "" {
		path = strings.ReplaceAll(fileIn.Path.Value, "//", "/")

	}

	if path != "" && fileIn.ParentId.Value == "" {
		parentID, err = resolvePathID(a.db, path, userId)
		if err != nil {
			return nil, &apiError{err: err, code: 404}
		}
		fileDB.ParentId = parentID

	} else if fileIn.ParentId.Value != "" {
		fileDB.ParentId = utils.Ptr(fileIn.ParentId.Value)
	}

	switch fileIn.Type {
	case api.FileTypeFolder:
		fileDB.MimeType = "drive/folder"
		fileDB.Parts = nil
	case api.FileTypeFile:
		if fileIn.ChannelId.Value == 0 {
			channelId, err = a.channelManager.CurrentChannel(ctx, userId)
			if err != nil {
				return nil, &apiError{err: err}
			}
		} else {
			channelId = fileIn.ChannelId.Value
		}
		fileDB.ChannelId = &channelId
		fileDB.MimeType = fileIn.MimeType.Value
		fileDB.Category = utils.Ptr(string(category.GetCategory(fileIn.Name)))

		// Handle parts - either from direct input or fetch by uploadId
		var parts []api.Part
		if len(fileIn.Parts) > 0 {
			parts = fileIn.Parts
		} else if fileIn.UploadId.Value != "" {
			uploadId = fileIn.UploadId.Value
			// Fetch parts from uploads table
			if err := a.db.Where("upload_id = ?", uploadId).Order("part_no").Find(&uploads).Error; err != nil {
				return nil, &apiError{err: err}
			}

			// Validate parts: sum of sizes must equal file size and no partId should be 0
			for _, upload := range uploads {
				if upload.PartId == 0 {
					return nil, &apiError{err: errors.New("invalid part: part_id cannot be zero"), code: 400}
				}
			}

			// Convert uploads to parts
			for _, upload := range uploads {
				parts = append(parts, api.Part{
					ID:   upload.PartId,
					Salt: api.NewOptString(upload.Salt),
				})
			}
		}

		if len(parts) > 0 {
			fileDB.Parts = utils.Ptr(datatypes.NewJSONSlice(mapParts(parts)))
		}

		fileDB.Size = utils.Ptr(fileIn.Size.Value)

		// Compute BLAKE3 tree hash from block hashes if uploadId is provided
		if uploadId != "" && len(uploads) > 0 {
			var allBlockHashes []byte
			for _, upload := range uploads {
				allBlockHashes = append(allBlockHashes, upload.BlockHashes...)
			}

			if len(allBlockHashes) > 0 {
				treeHashBytes := hash.ComputeTreeHash(allBlockHashes)
				treeHash := hash.SumToHex(treeHashBytes)
				fileDB.Hash = &treeHash
			}
		} else if fileIn.Size.Value == 0 {
			// For zero-length files, compute hash of empty data
			treeHashBytes := hash.ComputeTreeHash([]byte{})
			treeHash := hash.SumToHex(treeHashBytes)
			fileDB.Hash = &treeHash
		} else if uploadId == "" && len(parts) > 0 && !fileIn.Encrypted.Value {
			// Backwards-compatible dedup: parts were supplied directly (bypassing the
			// /uploads + uploadId flow), so no hash was computed above. Best-effort compute
			// one now by re-reading the content back from Telegram, so files created this
			// way can still be deduplicated. Never fails the create - just skips the hash.
			client, token, botID, cerr := ResolveUserClient(ctx, a.db, a.cache, &a.cnf.TG,
				a.channelManager, a.botSelector, userId, auth.GetJWTUser(ctx).TgSession)
			if cerr != nil {
				logging.FromContext(ctx).Error("dedup hash backfill: resolve client failed", zap.Error(cerr))
			} else if herr := tgc.RunWithAuth(ctx, client, token, func(ctx context.Context) error {
				h, err := ComputeFileContentHash(ctx, client, a.cache, &a.cnf.TG, botID, &fileDB)
				if err != nil {
					return err
				}
				fileDB.Hash = &h
				return nil
			}); herr != nil {
				logging.FromContext(ctx).Error("dedup hash backfill failed", zap.Error(herr))
			}
		}
	}
	fileDB.Name = fileIn.Name
	fileDB.Type = string(fileIn.Type)
	fileDB.UserId = userId
	fileDB.Status = "active"
	fileDB.Encrypted = utils.Ptr(fileIn.Encrypted.Value)
	if fileIn.UpdatedAt.IsSet() && !fileIn.UpdatedAt.Value.IsZero() {
		fileDB.UpdatedAt = utils.Ptr(fileIn.UpdatedAt.Value)
	} else {
		fileDB.UpdatedAt = utils.Ptr(time.Now().UTC())
	}

	// Check for deduplication: if file is not encrypted and has a hash, look for existing file with same hash
	if !*fileDB.Encrypted && fileDB.Hash != nil && *fileDB.Hash != "" && fileDB.Type == string(api.FileTypeFile) {
		existingFile, err := a.FindDeduplicateFile(ctx, userId, *fileDB.Hash)
		if err != nil {
			// Log error but don't fail the upload due to dedup check
			logging.FromContext(ctx).Error("dedup check failed", zap.Error(err))
		}
		if existingFile != nil {
			// Found a duplicate: set ReferencedFileId to point to the canonical file
			// This makes this file a reference to the existing file's Telegram messages
			fileDB.ReferencedFileId = utils.Ptr(existingFile.ID)
			// Keep the same Parts and ChannelId as the existing file
			fileDB.Parts = existingFile.Parts
			fileDB.ChannelId = existingFile.ChannelId
		}
	}

	// Use transaction to ensure file creation and upload cleanup are atomic
	err = a.db.Transaction(func(tx *gorm.DB) error {
		//For some reason, gorm conflict clauses are not working with partial index so using raw query
		if err := database.RetryTransientLock(ctx, database.DefaultLockRetryAttempts, func() error {
			return tx.Raw(`
				INSERT INTO teldrive.files (
					name, parent_id, user_id, mime_type, category, parts,
					size, type, encrypted, updated_at, channel_id, status, hash, referenced_file_id
				)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT (name, COALESCE(parent_id, '00000000-0000-0000-0000-000000000000'::uuid), user_id)
				WHERE status = 'active'
				DO UPDATE SET
					mime_type = EXCLUDED.mime_type,
					category = EXCLUDED.category,
					parts = EXCLUDED.parts,
					size = EXCLUDED.size,
					type = EXCLUDED.type,
					encrypted = EXCLUDED.encrypted,
					updated_at = EXCLUDED.updated_at,
					channel_id = EXCLUDED.channel_id,
					status = EXCLUDED.status,
					hash = EXCLUDED.hash,
					referenced_file_id = EXCLUDED.referenced_file_id
				RETURNING *
			`,
				fileDB.Name, fileDB.ParentId, fileDB.UserId, fileDB.MimeType,
				fileDB.Category, fileDB.Parts, fileDB.Size, fileDB.Type,
				fileDB.Encrypted, fileDB.UpdatedAt, fileDB.ChannelId, fileDB.Status,
				fileDB.Hash, fileDB.ReferencedFileId,
			).Scan(&fileDB).Error
		}); err != nil {
			return err
		}

		// Delete uploads after successful file creation
		if uploadId != "" {
			if err := tx.Where("upload_id = ?", uploadId).Delete(&models.Upload{}).Error; err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		return nil, &apiError{err: err}
	}

	if fileDB.ParentId != nil {
		parentID = fileDB.ParentId
	}

	a.events.Record(events.OpCreate, userId, &models.Source{
		ID:       fileDB.ID,
		Type:     fileDB.Type,
		Name:     fileDB.Name,
		ParentID: *parentID,
	})
	return mapper.ToFileOut(fileDB), nil
}

func (a *apiService) FilesCreateShare(ctx context.Context, req *api.FileShareCreate, params api.FilesCreateShareParams) error {
	userId := auth.GetUser(ctx)

	var fileShare models.FileShare

	if req.Password.Value != "" {
		bytes, err := bcrypt.GenerateFromPassword([]byte(req.Password.Value), bcrypt.MinCost)
		if err != nil {
			return &apiError{err: err}
		}
		fileShare.Password = utils.Ptr(string(bytes))
	}

	fileShare.FileId = params.ID
	if req.ExpiresAt.IsSet() {
		fileShare.ExpiresAt = utils.Ptr(req.ExpiresAt.Value)
	}
	fileShare.UserId = userId

	if err := a.db.Create(&fileShare).Error; err != nil {
		return &apiError{err: err}
	}

	return nil
}

func (a *apiService) deleteFilesBulk(ctx context.Context, db *gorm.DB, fileIds []string, userId int64) error {
	query := `
	WITH RECURSIVE target_folders AS (
		SELECT id FROM teldrive.files WHERE id IN (?) AND user_id = ?
		UNION ALL
		SELECT f.id FROM teldrive.files f JOIN target_folders tf ON f.parent_id = tf.id
	),
	mark_deleted AS (
		UPDATE teldrive.files SET status = 'pending_deletion'
		WHERE (parent_id IN (SELECT id FROM target_folders) OR id IN (?))
		AND type = 'file'
	)
	DELETE FROM teldrive.files WHERE id IN (SELECT id FROM target_folders) AND type = 'folder';
	`
	return database.RetryTransientLock(ctx, database.DefaultLockRetryAttempts, func() error {
		return db.Exec(query, fileIds, userId, fileIds).Error
	})
}

func (a *apiService) getFullPath(db *gorm.DB, fileID string) (string, error) {
	var path string
	query := `
	WITH RECURSIVE path_tree AS (
		SELECT id, parent_id, name, 0 as lvl FROM teldrive.files WHERE id = ?
		UNION ALL
		SELECT f.id, f.parent_id, f.name, pt.lvl + 1
		FROM teldrive.files f JOIN path_tree pt ON f.id = pt.parent_id
	)
	SELECT string_agg(name, '/' ORDER BY lvl DESC) FROM path_tree;
	`
	err := db.Raw(query, fileID).Scan(&path).Error
	if path != "" {
		path = "/" + path
	}
	return strings.TrimPrefix(path, "/root"), err
}

func (a *apiService) FilesDelete(ctx context.Context, req *api.FileDelete) error {
	userId := auth.GetUser(ctx)

	if len(req.Ids) == 0 {
		return &apiError{err: errors.New("ids should not be empty"), code: 409}
	}

	var fileDB models.File

	if err := a.db.Model(&models.File{}).Where("id = ?", req.Ids[0]).Where("user_id = ?", userId).
		First(&fileDB).Error; err != nil {
		return &apiError{err: err}
	}

	// Check for deduplication references before deletion
	// If this file has a hash and there are other files with the same hash,
	// log it for reference purposes (UI/client can be enhanced to show this info)
	if fileDB.Hash != nil && *fileDB.Hash != "" && !*fileDB.Encrypted {
		refCount, err := a.CountDedupReferences(ctx, fileDB.ID)
		if err != nil {
			logging.FromContext(ctx).Error("failed to count dedup references", zap.Error(err))
		}
		if refCount > 0 {
			logging.FromContext(ctx).Debug("deleting deduplicated file with references",
				zap.String("file_id", fileDB.ID),
				zap.String("file_hash", *fileDB.Hash),
				zap.Int64("ref_count", refCount),
			)
		}
	}

	if err := a.deleteFilesBulk(ctx, a.db, req.Ids, userId); err != nil {
		return &apiError{err: err}
	}

	keys := []string{}
	for _, id := range req.Ids {
		keys = append(keys, cache.KeyFile(id), cache.KeyFileMessages(id))
	}
	if len(keys) > 0 {
		a.cache.Delete(ctx, keys...)
	}

	var parentID string
	if fileDB.ParentId != nil {
		parentID = *fileDB.ParentId
	}

	a.events.Record(events.OpDelete, userId, &models.Source{
		ID:       fileDB.ID,
		Type:     fileDB.Type,
		Name:     fileDB.Name,
		ParentID: parentID,
	})

	return nil
}

func (a *apiService) FilesDeleteShare(ctx context.Context, params api.FilesDeleteShareParams) error {
	userId := auth.GetUser(ctx)

	var deletedShare models.FileShare

	if err := a.db.Clauses(clause.Returning{}).Where("file_id = ?", params.ID).Where("user_id = ?", userId).
		Delete(&deletedShare).Error; err != nil {
		return &apiError{err: err}
	}
	if deletedShare.ID != "" {
		a.cache.Delete(ctx, cache.KeyShare(deletedShare.ID))
	}

	return nil
}

func (a *apiService) FilesEditShare(ctx context.Context, req *api.FileShareCreate, params api.FilesEditShareParams) error {
	userId := auth.GetUser(ctx)

	var fileShareUpdate models.FileShare

	if req.Password.Value != "" {
		bytes, err := bcrypt.GenerateFromPassword([]byte(req.Password.Value), bcrypt.MinCost)
		if err != nil {
			return &apiError{err: err}
		}
		fileShareUpdate.Password = utils.Ptr(string(bytes))
	}
	if req.ExpiresAt.IsSet() {
		fileShareUpdate.ExpiresAt = utils.Ptr(req.ExpiresAt.Value)
	}

	if err := a.db.Model(&models.FileShare{}).Where("file_id = ?", params.ID).Where("user_id = ?", userId).
		Updates(fileShareUpdate).Error; err != nil {
		return &apiError{err: err}
	}

	return nil
}

func (a *apiService) FilesGetById(ctx context.Context, params api.FilesGetByIdParams) (*api.File, error) {
	var file models.File
	if err := a.db.Model(&models.File{}).Where("id = ?", params.ID).First(&file).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, &apiError{err: errors.New("file not found"), code: 404}
		}
		return nil, &apiError{err: err}
	}

	path, err := a.getFullPath(a.db, params.ID)
	if err != nil {
		return nil, &apiError{err: err}
	}

	res := mapper.ToFileOut(file)
	res.Path = api.NewOptString(path)
	if file.ChannelId != nil {
		res.ChannelId = api.NewOptInt64(*file.ChannelId)
	}

	return res, nil
}

func (a *apiService) collectFilesRecursive(db *gorm.DB, ids []string, userId int64) ([]models.File, error) {
	var files []models.File
	var toProcess []string = ids
	processed := make(map[string]bool)

	for len(toProcess) > 0 {
		var currentBatch []models.File
		if err := db.Model(&models.File{}).Where("id IN (?) AND user_id = ?", toProcess, userId).
			Where("status = ?", "active").Find(&currentBatch).Error; err != nil {
			return nil, err
		}

		toProcess = nil
		for _, file := range currentBatch {
			if processed[file.ID] {
				continue
			}
			processed[file.ID] = true

			if file.Type == "file" {
				files = append(files, file)
			} else if file.Type == "folder" {
				var childIds []string
				if err := db.Model(&models.File{}).Where("parent_id = ? AND user_id = ?", file.ID, userId).
					Where("status = ?", "active").Pluck("id", &childIds).Error; err != nil {
					return nil, err
				}
				toProcess = append(toProcess, childIds...)
			}
		}
	}

	return files, nil
}

func (a *apiService) buildRelativePath(db *gorm.DB, fileId string, rootFolderId string) (string, error) {
	var pathParts []string
	currentId := fileId

	for {
		var file models.File
		if err := db.Where("id = ?", currentId).First(&file).Error; err != nil {
			return "", err
		}

		pathParts = append([]string{file.Name}, pathParts...)

		if file.ParentId == nil || *file.ParentId == "" || *file.ParentId == rootFolderId {
			break
		}

		currentId = *file.ParentId
	}

	return strings.Join(pathParts, "/"), nil
}

func (a *apiService) getZipMetadata(db *gorm.DB, ids []string, userId int64) (string, bool, error) {
	if len(ids) != 1 {
		return "download.zip", false, nil
	}

	var item models.File
	if err := db.Where("id = ? AND user_id = ?", ids[0], userId).First(&item).Error; err != nil {
		return "download.zip", false, nil
	}

	if item.Type == "folder" {
		return fmt.Sprintf("%s.zip", item.Name), true, nil
	}

	if item.ParentId != nil {
		var parent models.File
		if err := db.Where("id = ?", *item.ParentId).First(&parent).Error; err == nil {
			return fmt.Sprintf("%s.zip", parent.Name), false, nil
		}
	}

	return "download.zip", false, nil
}

func (a *apiService) FilesDownloadZip(ctx context.Context, req *api.FileZipDownload) (*api.FilesDownloadZipOKHeaders, error) {
	if !a.cnf.Files.EnableZipDownload {
		return nil, &apiError{err: errors.New("zip download is disabled"), code: http.StatusForbidden}
	}

	userId := auth.GetUser(ctx)

	files, err := a.collectFilesRecursive(a.db, req.Ids, userId)
	if err != nil {
		return nil, &apiError{err: err}
	}
	if len(files) == 0 {
		return nil, &apiError{err: errors.New("no files found"), code: 404}
	}

	zipFilename, isSingleFolder, err := a.getZipMetadata(a.db, req.Ids, userId)
	if err != nil {
		return nil, &apiError{err: err}
	}

	tgSession := auth.GetJWTUser(ctx).TgSession

	pr, err := a.streamZip(ctx, userId, tgSession, files, req.Ids, isSingleFolder)
	if err != nil {
		return nil, err
	}

	return &api.FilesDownloadZipOKHeaders{
		ContentDisposition: mime.FormatMediaType("attachment", map[string]string{"filename": zipFilename}),
		Response:           api.FilesDownloadZipOK{Data: pr},
	}, nil
}

func (a *apiService) SharesDownloadZip(ctx context.Context, req *api.FileZipDownload, params api.SharesDownloadZipParams) (*api.SharesDownloadZipOKHeaders, error) {
	if !a.cnf.Files.EnableZipDownload {
		return nil, &apiError{err: errors.New("zip download is disabled"), code: http.StatusForbidden}
	}

	c := ctx.(*appcontext.Context)
	share, err := a.validFileShare(c.Request, params.ID)
	if err != nil {
		return nil, err
	}

	files, err := a.collectFilesRecursive(a.db, req.Ids, share.UserId)
	if err != nil {
		return nil, &apiError{err: err}
	}
	if len(files) == 0 {
		return nil, &apiError{err: errors.New("no files found"), code: 404}
	}

	zipFilename, isSingleFolder, err := a.getZipMetadata(a.db, req.Ids, share.UserId)
	if err != nil {
		return nil, &apiError{err: err}
	}

	pr, err := a.streamZip(ctx, share.UserId, "", files, req.Ids, isSingleFolder)
	if err != nil {
		return nil, err
	}

	return &api.SharesDownloadZipOKHeaders{
		ContentDisposition: mime.FormatMediaType("attachment", map[string]string{"filename": zipFilename}),
		Response:           api.SharesDownloadZipOK{Data: pr},
	}, nil
}

// streamZip resolves a Telegram client for userId (using tgSession as the
// fallback session when the user has no bots configured) and streams files
// into a zip archive, returning a reader that produces the archive as it's
// written. tgSession may be empty when the caller doesn't have access to the
// owning user's own session (e.g. a share viewer), in which case streaming
// requires the user to have bots configured.
// isSingleFolder indicates if we're downloading a single folder, in which case
// the folder name will be included in the zip paths.
func (a *apiService) streamZip(ctx context.Context, userId int64, tgSession string, files []models.File, selectedIds []string, isSingleFolder bool) (io.Reader, error) {
	tokens, err := a.channelManager.BotTokens(ctx, userId)
	if err != nil {
		return nil, &apiError{err: err}
	}
	if limit := a.cnf.TG.Stream.BotsLimit; limit > 0 && len(tokens) > limit {
		tokens = tokens[:limit]
	}

	var client *telegram.Client
	var token string
	if len(tokens) == 0 {
		if tgSession == "" {
			return nil, &apiError{err: errors.New("no bots configured for this account"), code: http.StatusForbidden}
		}
		client, err = tgc.AuthClient(ctx, &a.cnf.TG, tgSession, a.newMiddlewares(ctx, 5)...)
		if err != nil {
			return nil, &apiError{err: err}
		}
	} else {
		token, _, err = a.botSelector.Next(ctx, tgc.BotOpStream, userId, tokens)
		if err != nil {
			return nil, &apiError{err: err}
		}
		client, err = tgc.BotClient(ctx, a.db, a.cache, &a.cnf.TG, token, a.newMiddlewares(ctx, 5)...)
		if err != nil {
			return nil, &apiError{err: err}
		}
	}

	botID := strconv.FormatInt(userId, 10)
	if token != "" {
		if parts := strings.Split(token, ":"); len(parts) > 0 {
			botID = parts[0]
		}
	}

	pr, pw := io.Pipe()

	go func() {
		zw := zip.NewWriter(pw)
		logger := logging.Component("FILE").With(zap.Int64("user_id", userId))

		err := tgc.RunWithAuth(ctx, client, token, func(ctx context.Context) error {
			usedNames := make(map[string]int)
			var folderName string
			var folderID string
			if isSingleFolder && len(selectedIds) > 0 {
				var folder models.File
				if err := a.db.Where("id = ?", selectedIds[0]).First(&folder).Error; err == nil {
					folderName = folder.Name
					folderID = folder.ID
				}
			}

			for i := range files {
				file := &files[i]

				var name string
				if isSingleFolder && folderName != "" {
					relativePath, err := a.buildRelativePath(a.db, file.ID, folderID)
					if err == nil && relativePath != "" {
						name = fmt.Sprintf("%s/%s", folderName, relativePath)
					} else {
						name = fmt.Sprintf("%s/%s", folderName, file.Name)
					}
				} else {
					name = file.Name
				}

				if n, seen := usedNames[name]; seen {
					usedNames[name] = n + 1
					ext := strings.LastIndex(name, ".")
					if ext > 0 {
						name = fmt.Sprintf("%s (%d)%s", name[:ext], n+1, name[ext:])
					} else {
						name = fmt.Sprintf("%s (%d)", name, n+1)
					}
				} else {
					usedNames[name] = 0
				}

				if file.Size == nil || *file.Size == 0 {
					if _, err := zw.Create(name); err != nil {
						return err
					}
					continue
				}

				parts, err := getParts(ctx, client, a.cache, file)
				if err != nil {
					logger.Error("zip.parts_fetch_failed", zap.String("file_id", file.ID), zap.Error(err))
					return err
				}

				lr, err := reader.NewReader(ctx, client.API(), a.cache, file, parts, 0, *file.Size-1, &a.cnf.TG, botID)
				if err != nil {
					logger.Error("zip.reader_create_failed", zap.String("file_id", file.ID), zap.Error(err))
					return err
				}

				zf, err := zw.Create(name)
				if err != nil {
					lr.Close()
					return err
				}

				_, err = io.Copy(zf, lr)
				lr.Close()
				if err != nil {
					return err
				}
			}
			return nil
		})

		if err != nil {
			logger.Error("zip.stream_failed", zap.Error(err))
		}
		if cerr := zw.Close(); cerr != nil && err == nil {
			err = cerr
		}
		pw.CloseWithError(err)
	}()

	return pr, nil
}

func (a *apiService) FilesList(ctx context.Context, params api.FilesListParams) (*api.FileList, error) {
	userId := auth.GetUser(ctx)

	queryBuilder := &fileQueryBuilder{db: a.db}

	return queryBuilder.execute(&params, userId)
}

func (a *apiService) FilesMkdir(ctx context.Context, req *api.FileMkDir) error {
	userId := auth.GetUser(ctx)

	if err := database.RetryTransientLock(ctx, database.DefaultLockRetryAttempts, func() error {
		return a.db.Exec("select * from teldrive.create_directories(?, ?)", userId, req.Path).Error
	}); err != nil {
		return &apiError{err: err}
	}
	return nil
}

func (a *apiService) FilesMove(ctx context.Context, req *api.FileMove) error {
	userId := auth.GetUser(ctx)

	var destParentID *string

	if !isUUID(req.DestinationParent) {
		r, err := resolvePathID(a.db, req.DestinationParent, userId)
		if err != nil {
			return &apiError{err: err}
		}
		destParentID = r

	} else {
		destParentID = &req.DestinationParent
	}

	err := a.db.Transaction(func(tx *gorm.DB) error {
		var srcFile models.File
		if err := tx.Where("id = ? AND user_id = ?", req.Ids[0], userId).First(&srcFile).Error; err != nil {
			return err
		}
		if len(req.Ids) == 1 && req.DestinationName.Value != "" {
			var existing models.File
			query := tx.Where("name = ? AND user_id = ? AND status = 'active'",
				req.DestinationName.Value, userId)
			if destParentID == nil {
				query = query.Where("parent_id IS NULL")
			} else {
				query = query.Where("parent_id = ?", *destParentID)
			}

			if err := query.First(&existing).Error; err == nil {
				if srcFile.Type == "folder" && existing.Type == "folder" {
					if err := database.RetryTransientLock(ctx, database.DefaultLockRetryAttempts, func() error {
						return tx.Model(&models.File{}).
							Where("parent_id = ? AND status = 'active'", existing.ID).
							Where("name NOT IN (?)",
								tx.Model(&models.File{}).
									Select("name").
									Where("parent_id = ? AND status = 'active'", srcFile.ID),
							).
							Update("parent_id", srcFile.ID).Error
					}); err != nil {
						return err
					}
				}
				if err := a.deleteFilesBulk(ctx, tx, []string{existing.ID}, userId); err != nil {
					return err
				}
			}
			return database.RetryTransientLock(ctx, database.DefaultLockRetryAttempts, func() error {
				return tx.Model(&models.File{}).
					Where("id = ? AND user_id = ?", req.Ids[0], userId).
					Updates(map[string]any{
						"parent_id": destParentID,
						"name":      req.DestinationName.Value,
					}).Error
			})
		}
		items := pgtype.Array[string]{
			Elements: req.Ids,
			Valid:    true,
			Dims:     []pgtype.ArrayDimension{{Length: int32(len(req.Ids)), LowerBound: 1}},
		}
		if err := database.RetryTransientLock(ctx, database.DefaultLockRetryAttempts, func() error {
			return tx.Model(&models.File{}).Where("id = any(?)", items).Where("user_id = ?", userId).
				Update("parent_id", destParentID).Error
		}); err != nil {
			return err
		}

		var parentID string
		if srcFile.ParentId != nil {
			parentID = *srcFile.ParentId
		}

		var destParentIDStr string
		if destParentID != nil {
			destParentIDStr = *destParentID
		}

		a.events.Record(events.OpMove, userId, &models.Source{
			ID:           destParentIDStr,
			Type:         srcFile.Type,
			Name:         srcFile.Name,
			ParentID:     parentID,
			DestParentID: destParentIDStr,
		})
		return nil

	})
	if err != nil {
		return &apiError{err: err}
	}
	return nil

}

func (a *apiService) FilesShareByid(ctx context.Context, params api.FilesShareByidParams) (*api.FileShare, error) {
	userId := auth.GetUser(ctx)
	var result []models.FileShare

	notFoundErr := &apiError{err: errors.New("invalid share"), code: 404}
	if err := a.db.Model(&models.FileShare{}).Where("file_id = ?", params.ID).Where("user_id = ?", userId).
		Find(&result).Error; err != nil {
		if database.IsRecordNotFoundErr(err) {
			return nil, notFoundErr
		}
		return nil, &apiError{err: err}
	}

	if len(result) == 0 {
		return nil, notFoundErr
	}
	res := &api.FileShare{
		ID: result[0].ID,
	}
	if result[0].Password != nil {
		res.Protected = true
	}
	if result[0].ExpiresAt != nil {
		res.ExpiresAt = api.NewOptDateTime(*result[0].ExpiresAt)
	}
	return res, nil
}

func (a *apiService) FilesUpdate(ctx context.Context, req *api.FileUpdate, params api.FilesUpdateParams) (*api.File, error) {

	userId := auth.GetUser(ctx)

	updateDb := models.File{}
	isContentUpdate := false
	uploadId := ""
	var uploads []models.Upload

	if req.UploadId.IsSet() && req.UploadId.Value != "" {
		uploadId = req.UploadId.Value
		if err := a.db.Where("upload_id = ?", uploadId).Order("part_no").Find(&uploads).Error; err != nil {
			return nil, &apiError{err: err}
		}
		var totalSize int64
		for _, u := range uploads {
			req.Parts = append(req.Parts, api.Part{
				ID:   u.PartId,
				Salt: api.NewOptString(u.Salt),
			})
			totalSize += u.Size
		}
		if req.Size.Value == 0 {
			req.Size.SetTo(totalSize)
		}
	}

	if req.Name.IsSet() && req.Name.Value != "" {
		updateDb.Name = req.Name.Value
	}

	if req.ParentId.IsSet() && req.ParentId.Value != "" {
		updateDb.ParentId = utils.Ptr(req.ParentId.Value)
	}

	if req.ChannelId.IsSet() && req.ChannelId.Value != 0 {
		updateDb.ChannelId = utils.Ptr(req.ChannelId.Value)
	}

	if req.Size.IsSet() && req.Size.Value != 0 && len(req.Parts) > 0 {
		updateDb.Parts = utils.Ptr(datatypes.NewJSONSlice(mapParts(req.Parts)))
		updateDb.Size = utils.Ptr(req.Size.Value)
		isContentUpdate = true
	}
	if req.Size.IsSet() && req.Size.Value == 0 {
		updateDb.Size = utils.Ptr(req.Size.Value)
		isContentUpdate = true
	}

	if req.Encrypted.IsSet() {
		updateDb.Encrypted = utils.Ptr(req.Encrypted.Value)
		isContentUpdate = true
	}

	if req.Starred.IsSet() {
		updateDb.Starred = utils.Ptr(req.Starred.Value)
	}

	// Update UpdatedAt if content changed OR if explicitly set (e.g., SetModTime)
	if isContentUpdate || req.UpdatedAt.IsSet() {
		if req.UpdatedAt.IsSet() && !req.UpdatedAt.Value.IsZero() {
			updateDb.UpdatedAt = utils.Ptr(req.UpdatedAt.Value)
		} else {
			updateDb.UpdatedAt = utils.Ptr(time.Now().UTC())
		}
	}

	// Use transaction for atomic update
	var file models.File
	err := a.db.Transaction(func(tx *gorm.DB) error {
		// Fetch current file to check if it's a deduplicated copy (ReferencedFileId is set)
		var currentFile models.File
		if err := tx.Where("id = ?", params.ID).First(&currentFile).Error; err != nil {
			return err
		}

		// COPY-ON-WRITE: If content is being updated and this file is a reference to another file,
		// break the dedup link by clearing ReferencedFileId (make this file its own canonical copy)
		if isContentUpdate && currentFile.ReferencedFileId != nil && *currentFile.ReferencedFileId != "" {
			updateDb.ReferencedFileId = utils.Ptr("") // Clear reference, make this file canonical
		}

		// Compute BLAKE3 tree hash if uploadId provided
		if uploadId != "" && len(uploads) > 0 {
			var allBlockHashes []byte
			for _, upload := range uploads {
				allBlockHashes = append(allBlockHashes, upload.BlockHashes...)
			}

			if len(allBlockHashes) > 0 {
				treeHashBytes := hash.ComputeTreeHash(allBlockHashes)
				treeHash := hash.SumToHex(treeHashBytes)
				updateDb.Hash = &treeHash

				// DEDUP CHECK: After updating content with new hash, check if it matches another file's hash
				// If it does, we might be re-deduplicating with a different file (less common but possible)
				newHash := hash.SumToHex(treeHashBytes)
				existingFile, err := a.FindDeduplicateFile(ctx, currentFile.UserId, newHash)
				if err != nil {
					logging.FromContext(ctx).Error("dedup check on update failed", zap.Error(err))
				}
				if existingFile != nil && existingFile.ID != currentFile.ID {
					// Found a duplicate with new hash: point this file to the canonical copy
					updateDb.ReferencedFileId = utils.Ptr(existingFile.ID)
				}
			}
		}

		// Build update query - explicitly select UpdatedAt if it's the only change
		query := tx.Model(&models.File{}).Where("id = ?", params.ID)
		if req.UpdatedAt.IsSet() && !isContentUpdate {
			// Force update of updated_at field even when only metadata changes
			query = query.Select("updated_at")
		}
		if err := database.RetryTransientLock(ctx, database.DefaultLockRetryAttempts, func() error {
			return query.Updates(updateDb).Error
		}); err != nil {
			return err
		}

		// Delete uploads after successful update
		if uploadId != "" {
			if err := tx.Where("upload_id = ?", uploadId).Delete(&models.Upload{}).Error; err != nil {
				return err
			}
		}

		return tx.Where("id = ?", params.ID).First(&file).Error
	})

	if err != nil {
		return nil, &apiError{err: err}
	}

	keys := []string{cache.KeyFile(params.ID)}
	if len(req.Parts) > 0 {
		keys = append(keys, cache.KeyFileMessages(params.ID))
		a.cache.DeletePattern(ctx, cache.KeyFileLocationPattern(params.ID))
	}
	a.cache.Delete(ctx, keys...)

	var parentID string
	if file.ParentId != nil {
		parentID = *file.ParentId
	}

	a.events.Record(events.OpUpdate, userId, &models.Source{
		ID:       file.ID,
		Type:     file.Type,
		Name:     file.Name,
		ParentID: parentID,
	})
	return mapper.ToFileOut(file), nil
}

func (e *extendedService) FilesStream(w http.ResponseWriter, r *http.Request, fileId string, userId int64) {
	ctx := r.Context()
	logger := logging.Component("FILE").With(
		zap.String("file_id", fileId),
		zap.Int64("user_id", userId),
	)
	var (
		session *models.Session
		err     error
		user    *types.JWTClaims
	)
	if userId == 0 {

		authHash := r.URL.Query().Get("hash")
		if authHash == "" {
			cookie, err := r.Cookie(authCookieName)
			if err != nil {
				http.Error(w, "missing token or authash", http.StatusUnauthorized)
				return
			}
			user, err = auth.VerifyUser(ctx, e.api.db, e.api.cache, e.api.cnf.JWT.Secret, cookie.Value)
			if err != nil {
				http.Error(w, "invalid token", http.StatusUnauthorized)
			}
			userId, _ := strconv.ParseInt(user.Subject, 10, 64)
			session = &models.Session{UserId: userId, Session: user.TgSession}
		} else {
			session, err = auth.GetSessionByHash(ctx, e.api.db, e.api.cache, authHash)
			if err != nil {
				http.Error(w, "invalid hash", http.StatusBadRequest)
				return
			}
		}
	} else {
		session = &models.Session{UserId: userId}
	}

	file, err := cache.Fetch(ctx, e.api.cache, cache.Key("files", fileId), 0, func() (*models.File, error) {
		var result models.File
		if err := e.api.db.Model(&result).Where("id = ?", fileId).First(&result).Error; err != nil {
			return nil, err
		}
		return &result, nil
	})

	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Accept-Ranges", "bytes")

	var start, end int64

	rangeHeader := r.Header.Get("Range")
	contentType := defaultContentType

	if file.MimeType != "" {
		contentType = file.MimeType
	}

	if file.Size == nil || *file.Size == 0 {
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Content-Length", "0")
		w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": file.Name}))
		w.WriteHeader(http.StatusOK)
		return
	}

	status := http.StatusOK
	if rangeHeader == "" {
		start = 0
		end = *file.Size - 1
	} else {
		ranges, err := http_range.Parse(rangeHeader, *file.Size)
		if err == http_range.ErrNoOverlap {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", *file.Size))
			http.Error(w, http_range.ErrNoOverlap.Error(), http.StatusRequestedRangeNotSatisfiable)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if len(ranges) > 1 {
			http.Error(w, "multiple ranges are not supported", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		start = ranges[0].Start
		end = ranges[0].End
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, *file.Size))
		status = http.StatusPartialContent

	}

	contentLength := end - start + 1

	w.Header().Set("Content-Type", contentType)

	w.Header().Set("Content-Length", strconv.FormatInt(contentLength, 10))
	w.Header().Set("ETag", fmt.Sprintf("\"%s\"", md5.FromString(fileId+strconv.FormatInt(*file.Size, 10))))
	w.Header().Set("Last-Modified", file.UpdatedAt.UTC().Format(http.TimeFormat))

	disposition := "inline"

	download := r.URL.Query().Get("download") == "1"

	if download {
		disposition = "attachment"
	}

	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": file.Name}))

	w.WriteHeader(status)

	if r.Method == http.MethodHead {
		return
	}

	tokens, err := e.api.channelManager.BotTokens(ctx, session.UserId)

	if err != nil {
		logger.Error("stream.bots_fetch_failed", zap.Error(err))
		http.Error(w, "failed to get bots", http.StatusInternalServerError)
		return
	}

	// Limit the number of bots used for streaming if configured
	if limit := e.api.cnf.TG.Stream.BotsLimit; limit > 0 && len(tokens) > limit {
		tokens = tokens[:limit]
	}

	var (
		lr     io.ReadCloser
		client *telegram.Client
		token  string
	)

	if len(tokens) == 0 {
		client, err = tgc.AuthClient(ctx, &e.api.cnf.TG, session.Session, e.api.newMiddlewares(ctx, 5)...)
		if err != nil {
			logger.Error("stream.auth_client_failed", zap.Error(err))
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

	} else {
		token, _, err = e.api.botSelector.Next(ctx, tgc.BotOpStream, session.UserId, tokens)
		if err != nil {
			logger.Error("stream.bot_selection_failed", zap.Error(err))
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		client, err = tgc.BotClient(ctx, e.api.db, e.api.cache, &e.api.cnf.TG, token, e.api.newMiddlewares(ctx, 5)...)
		if err != nil {
			logger.Error("stream.bot_client_failed", zap.Error(err))
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	botID := strconv.FormatInt(session.UserId, 10)
	if token != "" {
		parts := strings.Split(token, ":")
		if len(parts) > 0 {
			botID = parts[0]
		}
	}

	if r.Method != http.MethodHead {
		handleStream := func() error {
			parts, err := getParts(ctx, client, e.api.cache, file)
			if err != nil {
				logger.Error("stream.parts_fetch_failed", zap.Error(err))
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return nil
			}

			lr, err = reader.NewReader(ctx,
				client.API(),
				e.api.cache,
				file,
				parts,
				start,
				end,
				&e.api.cnf.TG,
				botID,
			)

			if err != nil {
				logger.Error("stream.reader_create_failed", zap.Error(err))
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return nil
			}
			if lr == nil {
				logger.Error("stream.reader_nil")
				http.Error(w, "failed to initialise reader", http.StatusInternalServerError)
				return nil
			}

			_, err = io.CopyN(w, lr, contentLength)
			if err != nil {
				lr.Close()
			}
			return nil
		}

		tgc.RunWithAuth(ctx, client, token, func(ctx context.Context) error {
			return handleStream()
		})

	}
}

func (e *extendedService) SharesStream(w http.ResponseWriter, r *http.Request, shareId, fileId string) {
	share, err := e.api.validFileShare(r, shareId)
	if err != nil && errors.Is(err, ErrEmptyAuth) {
		w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	e.FilesStream(w, r, fileId, share.UserId)
}

func (a *apiService) FilesStream(ctx context.Context, params api.FilesStreamParams) (api.FilesStreamRes, error) {
	return nil, nil
}

func (a *apiService) SharesStream(ctx context.Context, params api.SharesStreamParams) (api.SharesStreamRes, error) {
	return nil, nil
}

func mapParts(_parts []api.Part) []api.Part {
	return utils.Map(_parts, func(part api.Part) api.Part {
		p := api.Part{ID: part.ID}
		if part.Salt.Value != "" {
			p.Salt = part.Salt
		}
		return p
	})

}
