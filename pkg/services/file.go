package services

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
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
	"github.com/tgdrive/teldrive/internal/filecache"
	"github.com/tgdrive/teldrive/internal/hash"
	"github.com/tgdrive/teldrive/internal/http_range"
	"github.com/tgdrive/teldrive/internal/logging"
	"github.com/tgdrive/teldrive/internal/md5"
	"github.com/tgdrive/teldrive/internal/psdconv"
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

func init() {
	// Not in the platform MIME database on most systems, and browsers report
	// an empty type for it on upload, so passes would otherwise stream as
	// octet-stream and never hand off to Wallet on iOS and macOS.
	_ = mime.AddExtensionType(".pkpass", "application/vnd.apple.pkpass")
	// Coverage for .psd in /etc/mime.types is distribution-dependent, so register
	// it here rather than relying on the system table being populated.
	_ = mime.AddExtensionType(".psd", "image/vnd.adobe.photoshop")

	// 3D model formats — browsers send empty or octet-stream for all of these.
	_ = mime.AddExtensionType(".fbx", "application/octet-stream")
	_ = mime.AddExtensionType(".gltf", "model/gltf+json")
	_ = mime.AddExtensionType(".glb", "model/gltf-binary")
	_ = mime.AddExtensionType(".obj", "model/obj")
	_ = mime.AddExtensionType(".stl", "model/stl")
	_ = mime.AddExtensionType(".3ds", "application/x-3ds")
	_ = mime.AddExtensionType(".ply", "application/x-ply")
	_ = mime.AddExtensionType(".dae", "model/vnd.collada+xml")
	_ = mime.AddExtensionType(".wrl", "model/vrml")
	_ = mime.AddExtensionType(".3mf", "application/vnd.ms-package.3dmanufacturing-3dmodel+xml")
	_ = mime.AddExtensionType(".usdz", "model/vnd.usdz+zip")

	// Unity packages — tar.gz archives with a specific internal structure.
	_ = mime.AddExtensionType(".unitypackage", "application/x-unitypackage")
}

// contentTypeFor falls back to the file extension when the stored MIME type is
// missing or the generic default, which is what clients send for any extension
// the browser doesn't recognize.
func contentTypeFor(name, mimeType string) string {
	if mimeType != "" && mimeType != defaultContentType {
		return mimeType
	}
	if byExtension := mime.TypeByExtension(filepath.Ext(name)); byExtension != "" {
		return byExtension
	}
	return defaultContentType
}

func isUUID(str string) bool {
	_, err := uuid.Parse(str)
	return err == nil
}

// blockHashesCover reports whether uploads carry a complete set of block hashes
// for parts, i.e. whether concatenating them yields the block hashes of the
// whole file and so a tree hash that matches what a client computes locally.
//
// It requires one upload row per part: a client may send parts explicitly
// alongside its uploadId, and only when the two line up do the recorded block
// hashes describe exactly the content being committed. A single part uploaded
// with hashing disabled leaves a gap that would silently produce a wrong hash,
// so any missing block hash disqualifies the whole set.
func blockHashesCover(uploads []models.Upload, parts []api.Part) bool {
	if len(uploads) == 0 || len(uploads) != len(parts) {
		return false
	}
	for _, upload := range uploads {
		if len(upload.BlockHashes) == 0 {
			return false
		}
	}
	return true
}

// FindDeduplicateFile finds an existing file with the same hash for a user
// Returns the file if found, or nil if not found
// Only considers non-encrypted files and active status
//
// Rows that are themselves deduplicated copies sort last, so a new copy links to
// a file that actually owns its Telegram parts rather than chaining through
// another reference. Folders are excluded: they carry no content to share.
func (a *apiService) FindDeduplicateFile(ctx context.Context, userId int64, fileHash string) (*models.File, error) {
	if fileHash == "" {
		return nil, nil // No hash provided, no dedup check
	}

	var file models.File
	if err := a.db.Where(
		"user_id = ? AND hash = ? AND encrypted = false AND status = 'active' AND type = 'file'",
		userId, fileHash,
	).Order("referenced_file_id IS NOT NULL, created_at ASC"). // Prefer a canonical, oldest first
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

	client, cerr := tgc.AuthClient(ctx, &a.cnf.TG, auth.GetJWTUser(ctx).TgSession, a.newMiddlewares(ctx, 5)...)
	if cerr != nil {
		return nil, &apiError{err: cerr}
	}

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

	// Identity and flags are assigned before the type switch: the dedup hash
	// backfill below reads content back from Telegram through helpers that expect
	// a fully populated file (notably Encrypted, which they dereference).
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

	switch fileIn.Type {
	case api.FileTypeFolder:
		fileDB.MimeType = "drive/folder"
		fileDB.Parts = nil
	case api.FileTypeFile:
		// Idempotent replay: if this session was already committed to an
		// active file and has no live parts, return that file instead of a
		// 409. A 409 makes the client retry the whole upload, which is what
		// duplicates the messages in the Telegram storage channel.
		if fileIn.UploadId.Value != "" && len(fileIn.Parts) == 0 {
			committed, cerr := a.committedUploadFile(ctx, userId, fileIn.UploadId.Value)
			if cerr != nil {
				return nil, &apiError{err: cerr}
			}
			if committed != nil && (fileIn.Size.Value == 0 || (committed.Size != nil && *committed.Size == fileIn.Size.Value)) {
				var live []models.Upload
				if err := a.db.WithContext(ctx).
					Where("upload_id = ? AND user_id = ?", fileIn.UploadId.Value, userId).
					Order("part_no").Find(&live).Error; err != nil {
					return nil, &apiError{err: err}
				}
				// No live rows is a plain replay. Malformed rows - duplicate
				// part numbers left by late retries of a part POST - are
				// replay residue too: they can never form a valid session and
				// would fail validation with 409 on every retry.
				if len(live) == 0 || !uploadPartsAreContiguous(live) {
					if len(live) > 0 {
						if err := a.retireStaleUploadSession(ctx, userId, fileIn.UploadId.Value); err != nil {
							return nil, &apiError{err: err}
						}
					}
					logging.FromContext(ctx).Info("files.create.replay",
						zap.String("upload_id", fileIn.UploadId.Value),
						zap.String("file_id", committed.ID))
					return mapper.ToFileOut(*committed), nil
				}
			}
		}
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

		// Load the upload session whenever the client names one, even if it also
		// sent the parts list explicitly. The rows carry the per-part block hashes
		// recorded during upload, which are what makes the tree hash below cheap.
		if fileIn.UploadId.Value != "" {
			uploadId = fileIn.UploadId.Value
			// Fetch parts from uploads table. Scoped to the caller: upload_id is a
			// client-computed value, so an unscoped lookup would let anyone who
			// supplies someone else's id commit their parts - and their block
			// hashes - into a file of their own.
			if err := a.db.Where("upload_id = ? AND user_id = ?", uploadId, userId).Order("part_no").Find(&uploads).Error; err != nil {
				return nil, &apiError{err: err}
			}

			// Validate parts: sum of sizes must equal file size and no partId should be 0
			for _, upload := range uploads {
				if upload.PartId == 0 {
					return nil, &apiError{err: errors.New("invalid part: part_id cannot be zero"), code: 400}
				}
			}

			// Reject a create whose upload session has no parts for a non-zero file.
			if len(uploads) == 0 && fileIn.Size.Value > 0 {
				return nil, &apiError{err: errors.New("upload session has no parts; upload may still be in progress"), code: 409}
			}

			// Verify part numbers are contiguous from 1 and their sizes sum to
			// the declared file size. A gap means a part upload failed silently;
			// a size mismatch means partial upload or a stale session.
			if len(uploads) > 0 {
				var partsTotal int64
				for i, upload := range uploads {
					if upload.PartNo != i+1 {
						return nil, &apiError{err: fmt.Errorf("upload session is incomplete: expected part %d, found %d", i+1, upload.PartNo), code: 409}
					}
					partsTotal += upload.Size
				}
				if fileIn.Size.Value > 0 && partsTotal != fileIn.Size.Value && !(uploads[0].Encrypted && partsTotal > fileIn.Size.Value) {
					return nil, &apiError{err: fmt.Errorf("upload size mismatch: parts total %d, declared %d", partsTotal, fileIn.Size.Value), code: 409}
				}
			}
		}

		// Handle parts - either from direct input or fetch by uploadId
		var parts []api.Part
		if len(fileIn.Parts) > 0 {
			parts = fileIn.Parts
		} else {
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

		switch {
		case blockHashesCover(uploads, parts):
			// Cheap path: the tree hash is built from the block hashes computed on
			// the plaintext as each part streamed through /uploads.
			var allBlockHashes []byte
			for _, upload := range uploads {
				allBlockHashes = append(allBlockHashes, upload.BlockHashes...)
			}
			treeHash := hash.SumToHex(hash.ComputeTreeHash(allBlockHashes))
			fileDB.Hash = &treeHash

		case fileIn.Size.Value == 0:
			// For zero-length files, compute hash of empty data
			treeHash := hash.SumToHex(hash.ComputeTreeHash([]byte{}))
			fileDB.Hash = &treeHash

		case uploadId == "" && len(parts) > 0 && !fileIn.Encrypted.Value:
			// Backwards-compatible dedup: an older client committed the file by
			// supplying parts directly, with no upload session to read block hashes
			// from. Best-effort compute the hash now by re-reading the content back
			// from Telegram, so files created this way can still be deduplicated.
			// Never fails the create - just skips the hash.
			//
			// A client that did name an upload session is not sent down this path: the
			// only way it can have no block hashes on record is by having asked for
			// hashing=false, and silently re-reading the whole file back from Telegram
			// would defeat the point of that request. Such files are left unhashed for
			// `deduplicate --backfill` to pick up later.
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

	// Use transaction to ensure dedup check + file creation + upload cleanup are atomic
	err = a.db.Transaction(func(tx *gorm.DB) error {
		// Check for deduplication inside the transaction to prevent race conditions
		// when concurrent uploads of the same content arrive simultaneously.
		// Uses raw query with FOR UPDATE to lock the canonical row, serializing
		// concurrent dedup checks for the same hash.
		if !fileDB.IsEncrypted() && fileDB.Hash != nil && *fileDB.Hash != "" && fileDB.Type == string(api.FileTypeFile) {
			var existingFile models.File
			if err := tx.Raw(`
				SELECT * FROM teldrive.files
				WHERE user_id = ? AND hash = ? AND status = 'active' AND encrypted = false
				ORDER BY created_at ASC
				LIMIT 1
				FOR UPDATE SKIP LOCKED
			`, userId, *fileDB.Hash).Scan(&existingFile).Error; err == nil && existingFile.ID != "" {
				// Found a duplicate: reference the canonical file's Telegram data
				fileDB.ReferencedFileId = utils.Ptr(existingFile.ID)
				fileDB.Parts = existingFile.Parts
				fileDB.ChannelId = existingFile.ChannelId
			}
		}

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

		// Snapshot the session before dropping its rows, so a replay of the
		// same session can be answered without a second Telegram upload.
		// This runs inside the file-creation transaction, so a failed create
		// never records a commit.
		if uploadId != "" {
			if err := recordUploadCommit(tx, userId, uploadId, fileDB.ID, fileIn.Size.Value, uploads); err != nil {
				return err
			}
			if err := tx.Where("upload_id = ? AND user_id = ?", uploadId, userId).Delete(&models.Upload{}).Error; err != nil {
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

	blockDirectLink := req.BlockDirectLink.Or(false)
	alwaysDirectLink := req.AlwaysDirectLink.Or(false)
	if blockDirectLink && alwaysDirectLink {
		alwaysDirectLink = false // block wins when both are requested at once
	}
	fileShare.BlockDirectLink = blockDirectLink
	fileShare.AlwaysDirectLink = alwaysDirectLink
	fileShare.AllowZipDownload = req.AllowZipDownload.Or(false)

	if req.ShortCode.IsSet() && req.ShortCode.Value == "" {
		// Auto-generate: retry on a unique-constraint conflict rather than
		// pre-checking availability, to avoid a check-then-insert race.
		for attempt := 0; attempt < shortCodeGenRetries; attempt++ {
			code, err := generateShortCode(a.cnf.Shortlinks.CodeLength)
			if err != nil {
				return &apiError{err: err}
			}
			fileShare.ShortCode = &code
			if err := a.db.Create(&fileShare).Error; err != nil {
				if database.IsKeyConflictErr(err) {
					continue
				}
				return &apiError{err: err}
			}
			return nil
		}
		return &apiError{err: errors.New("failed to generate a unique shortlink code")}
	}

	if req.ShortCode.IsSet() {
		code := req.ShortCode.Value
		if err := validateShortCode(code); err != nil {
			return &apiError{err: err, code: http.StatusBadRequest}
		}
		fileShare.ShortCode = &code
	}

	if err := a.db.Create(&fileShare).Error; err != nil {
		if database.IsKeyConflictErr(err) {
			return &apiError{err: ErrShortCodeTaken, code: http.StatusConflict}
		}
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
	if fileDB.Hash != nil && *fileDB.Hash != "" && !fileDB.IsEncrypted() {
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
		keys := []string{cache.KeyShare(deletedShare.ID)}
		if deletedShare.ShortCode != nil {
			keys = append(keys, cache.KeyShare(*deletedShare.ShortCode))
		}
		a.cache.Delete(ctx, keys...)
	}

	return nil
}

// FilesEditShare reads the share first (rather than a blind
// Where(...).Updates(...)) for two reasons: it needs the *pre-update* id and
// short code to invalidate the right cache entries below, and it needs the
// existing toggle values to re-normalize the mutually-exclusive
// block/always pair when only one side of the pair is present in the
// request. Edits are applied via a map, not a struct, because GORM's
// struct-based Updates silently skips zero-value fields (false, nil) —
// which would make it impossible to ever turn a toggle back off or clear a
// short code.
func (a *apiService) FilesEditShare(ctx context.Context, req *api.FileShareCreate, params api.FilesEditShareParams) error {
	userId := auth.GetUser(ctx)

	var existing models.FileShare
	if err := a.db.Where("file_id = ?", params.ID).Where("user_id = ?", userId).First(&existing).Error; err != nil {
		if database.IsRecordNotFoundErr(err) {
			return &apiError{err: database.ErrNotFound, code: http.StatusNotFound}
		}
		return &apiError{err: err}
	}

	updates := map[string]any{}

	if req.Password.Value != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(req.Password.Value), bcrypt.MinCost)
		if err != nil {
			return &apiError{err: err}
		}
		updates["password"] = string(hash)
	}
	if req.ExpiresAt.IsSet() {
		updates["expires_at"] = req.ExpiresAt.Value
	}

	blockDirectLink, alwaysDirectLink := existing.BlockDirectLink, existing.AlwaysDirectLink
	if req.BlockDirectLink.IsSet() {
		blockDirectLink = req.BlockDirectLink.Value
	}
	if req.AlwaysDirectLink.IsSet() {
		alwaysDirectLink = req.AlwaysDirectLink.Value
	}
	if blockDirectLink && alwaysDirectLink {
		alwaysDirectLink = false // block wins when both are requested at once
	}
	if req.BlockDirectLink.IsSet() || req.AlwaysDirectLink.IsSet() {
		updates["block_direct_link"] = blockDirectLink
		updates["always_direct_link"] = alwaysDirectLink
	}
	if req.AllowZipDownload.IsSet() {
		updates["allow_zip_download"] = req.AllowZipDownload.Value
	}

	clearCode := req.ClearShortCode.Or(false)
	if clearCode {
		updates["short_code"] = nil
		updates["block_direct_link"] = false
		updates["always_direct_link"] = false
		updates["allow_zip_download"] = false
	}

	// applyUpdate runs the actual UPDATE, optionally pinning short_code to
	// code. It's called more than once only when auto-generating a code and
	// retrying past a unique-constraint conflict.
	applyUpdate := func(code *string) error {
		u := make(map[string]any, len(updates)+1)
		for k, v := range updates {
			u[k] = v
		}
		if code != nil {
			u["short_code"] = *code
		}
		if len(u) == 0 {
			return nil
		}
		return a.db.Model(&models.FileShare{}).Where("id = ?", existing.ID).Updates(u).Error
	}

	var newCode *string

	switch {
	case clearCode:
		if err := applyUpdate(nil); err != nil {
			return &apiError{err: err}
		}
	case req.ShortCode.IsSet() && req.ShortCode.Value == "":
		var assigned string
		for attempt := 0; attempt < shortCodeGenRetries; attempt++ {
			gen, err := generateShortCode(a.cnf.Shortlinks.CodeLength)
			if err != nil {
				return &apiError{err: err}
			}
			if err := applyUpdate(&gen); err != nil {
				if database.IsKeyConflictErr(err) {
					continue
				}
				return &apiError{err: err}
			}
			assigned = gen
			break
		}
		if assigned == "" {
			return &apiError{err: errors.New("failed to generate a unique shortlink code")}
		}
		newCode = &assigned
	case req.ShortCode.IsSet():
		code := req.ShortCode.Value
		if err := validateShortCode(code); err != nil {
			return &apiError{err: err, code: http.StatusBadRequest}
		}
		if err := applyUpdate(&code); err != nil {
			if database.IsKeyConflictErr(err) {
				return &apiError{err: ErrShortCodeTaken, code: http.StatusConflict}
			}
			return &apiError{err: err}
		}
		newCode = &code
	default:
		if err := applyUpdate(nil); err != nil {
			return &apiError{err: err}
		}
	}

	a.cache.Delete(ctx, cache.KeyShare(existing.ID))
	if existing.ShortCode != nil {
		a.cache.Delete(ctx, cache.KeyShare(*existing.ShortCode))
	}
	if newCode != nil {
		a.cache.Delete(ctx, cache.KeyShare(*newCode))
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

// pathWithinShare reports whether filePath is the shared node itself or lives
// underneath it. Both paths come from getFullPath, so they're directly
// comparable. A share rooted at "" covers the whole drive and matches anything.
func pathWithinShare(sharePath, filePath string) bool {
	sharePath = strings.TrimSuffix(sharePath, "/")
	if sharePath == "" {
		return true
	}
	return filePath == sharePath || strings.HasPrefix(filePath, sharePath+"/")
}

// checkZipLimits rejects an archive that's too large to be worth streaming,
// before a single byte of the response has been written. Either limit is
// disabled when zero.
func checkZipLimits(files []models.File, maxFiles int, maxSize int64) error {
	if maxFiles > 0 && len(files) > maxFiles {
		return fmt.Errorf("zip download has %d files, limit is %d", len(files), maxFiles)
	}
	if maxSize > 0 {
		var total int64
		for i := range files {
			if files[i].Size != nil {
				total += *files[i].Size
			}
		}
		if total > maxSize {
			return fmt.Errorf("zip download is %d bytes, limit is %d", total, maxSize)
		}
	}
	return nil
}

// zipCompressionMethod picks Store over Deflate for payloads that are already
// compressed. Deflating them costs CPU for every byte and saves nothing.
func zipCompressionMethod(name string) uint16 {
	switch category.GetCategory(name) {
	case category.Image, category.Video, category.Audio, category.Archive:
		return zip.Store
	default:
		return zip.Deflate
	}
}

// assertIdsWithinShare rejects any requested id that isn't covered by the share.
// Without this a viewer could name any file id the share owner happens to own
// and have it zipped for them, regardless of what was actually shared.
func (a *apiService) assertIdsWithinShare(db *gorm.DB, share *fileShare, ids []string) error {
	if share.Type == api.FileShareInfoTypeFile {
		for _, id := range ids {
			if id != share.FileId {
				return &apiError{err: errors.New("file is not part of this share"), code: http.StatusForbidden}
			}
		}
		return nil
	}

	notShared := &apiError{err: errors.New("file is not part of this share"), code: http.StatusForbidden}

	for _, id := range ids {
		// Resolve the file first. An id belonging to another user, or to nothing
		// at all, gets the same answer as one outside the share — a viewer has no
		// business learning which of those it was.
		var file models.File
		if err := db.Where("id = ? AND user_id = ?", id, share.UserId).First(&file).Error; err != nil {
			if database.IsRecordNotFoundErr(err) {
				return notShared
			}
			return &apiError{err: err}
		}

		path, err := a.getFullPath(db, id)
		if err != nil {
			return &apiError{err: err}
		}
		if !pathWithinShare(share.Path, path) {
			return notShared
		}
	}
	return nil
}

// shareZipName names the archive after the share itself. The owner-facing
// getZipMetadata falls back to the parent folder's name, which would leak a
// folder name from outside the share.
func shareZipName(share *fileShare, ids []string) (string, bool) {
	if len(ids) == 1 && ids[0] == share.FileId {
		return fmt.Sprintf("%s.zip", share.Name), share.Type == api.FileShareInfoTypeFolder
	}
	return "download.zip", false
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
	if err := checkZipLimits(files, a.cnf.Files.ZipMaxFiles, a.cnf.Files.ZipMaxSize); err != nil {
		return nil, &apiError{err: err, code: http.StatusRequestEntityTooLarge}
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
		// Let the browser put up its own password prompt, the same way a
		// single-file share download does. The UI submits this request as a form
		// navigation, so it has no way to set the header itself.
		if errors.Is(err, ErrEmptyAuth) {
			c.Writer.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
		}
		return nil, err
	}

	if share.Type == api.FileShareInfoTypeFolder && !share.AllowZipDownload {
		return nil, &apiError{err: errors.New("zip download is not allowed for this share"), code: http.StatusForbidden}
	}

	if err := a.assertIdsWithinShare(a.db, share, req.Ids); err != nil {
		return nil, err
	}

	files, err := a.collectFilesRecursive(a.db, req.Ids, share.UserId)
	if err != nil {
		return nil, &apiError{err: err}
	}
	if len(files) == 0 {
		return nil, &apiError{err: errors.New("no files found"), code: 404}
	}
	if err := checkZipLimits(files, a.cnf.Files.ZipMaxFiles, a.cnf.Files.ZipMaxSize); err != nil {
		return nil, &apiError{err: err, code: http.StatusRequestEntityTooLarge}
	}

	zipFilename, isSingleFolder := shareZipName(share, req.Ids)

	// A share viewer never carries the owner's session, so fall back to the
	// owner's own. Without this, zipping a share fails outright whenever the
	// owner has no bots configured.
	var ownerSession string
	if session, err := auth.GetSessionByUserId(ctx, a.db, a.cache, share.UserId); err == nil {
		ownerSession = session.Session
	}

	pr, err := a.streamZip(ctx, share.UserId, ownerSession, files, req.Ids, isSingleFolder)
	if err != nil {
		return nil, err
	}

	return &api.SharesDownloadZipOKHeaders{
		ContentDisposition: mime.FormatMediaType("attachment", map[string]string{"filename": zipFilename}),
		Response:           api.SharesDownloadZipOK{Data: pr},
	}, nil
}

// SharesDownloadZipDirect is the body-less GET counterpart to
// SharesDownloadZip: it implies "zip the whole share" (no explicit file ids
// to validate), so it can be the target of a redirect — a 302 can't carry a
// POST body. This is what the shortlink resolver redirects to for a
// folder-type shortlink with AllowZipDownload enabled.
func (a *apiService) SharesDownloadZipDirect(ctx context.Context, params api.SharesDownloadZipDirectParams) (*api.SharesDownloadZipDirectOKHeaders, error) {
	if !a.cnf.Files.EnableZipDownload {
		return nil, &apiError{err: errors.New("zip download is disabled"), code: http.StatusForbidden}
	}

	c := ctx.(*appcontext.Context)
	share, err := a.validFileShare(c.Request, params.ID)
	if err != nil {
		if errors.Is(err, ErrEmptyAuth) {
			c.Writer.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
		}
		return nil, err
	}

	if share.Type == api.FileShareInfoTypeFolder && !share.AllowZipDownload {
		return nil, &apiError{err: errors.New("zip download is not allowed for this share"), code: http.StatusForbidden}
	}

	ids := []string{share.FileId}

	files, err := a.collectFilesRecursive(a.db, ids, share.UserId)
	if err != nil {
		return nil, &apiError{err: err}
	}
	if len(files) == 0 {
		return nil, &apiError{err: errors.New("no files found"), code: 404}
	}
	if err := checkZipLimits(files, a.cnf.Files.ZipMaxFiles, a.cnf.Files.ZipMaxSize); err != nil {
		return nil, &apiError{err: err, code: http.StatusRequestEntityTooLarge}
	}

	zipFilename, isSingleFolder := shareZipName(share, ids)

	var ownerSession string
	if session, err := auth.GetSessionByUserId(ctx, a.db, a.cache, share.UserId); err == nil {
		ownerSession = session.Session
	}

	pr, err := a.streamZip(ctx, share.UserId, ownerSession, files, ids, isSingleFolder)
	if err != nil {
		return nil, err
	}

	return &api.SharesDownloadZipDirectOKHeaders{
		ContentDisposition: mime.FormatMediaType("attachment", map[string]string{"filename": zipFilename}),
		Response:           api.SharesDownloadZipDirectOK{Data: pr},
	}, nil
}

// streamZip resolves a Telegram client for userId (using tgSession as the
// fallback session when the user has no bots configured) and streams files
// into a zip archive, returning a reader that produces the archive as it's
// written. tgSession may be empty when neither bots nor a stored session could
// be found for the owning user, in which case streaming isn't possible.
// isSingleFolder indicates if we're downloading a single folder, in which case
// the folder name will be included in the zip paths.
func (a *apiService) streamZip(ctx context.Context, userId int64, tgSession string, files []models.File, selectedIds []string, isSingleFolder bool) (io.Reader, error) {
	// Each archive holds a Telegram client for as long as it takes to stream,
	// which can be the full write timeout. Turn away the excess rather than
	// queueing it: a request that blocks here occupies a connection anyway.
	if a.zipSlots != nil {
		select {
		case a.zipSlots <- struct{}{}:
		default:
			return nil, &apiError{err: errors.New("too many zip downloads in progress, try again shortly"),
				code: http.StatusServiceUnavailable}
		}
	}
	releaseSlot := func() {
		if a.zipSlots != nil {
			<-a.zipSlots
		}
	}
	// The slot belongs to the streaming goroutine once it starts; until then any
	// error return has to give it back.
	handedOff := false
	defer func() {
		if !handedOff {
			releaseSlot()
		}
	}()

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
	handedOff = true

	go func() {
		defer releaseSlot()
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

				zf, err := zw.CreateHeader(&zip.FileHeader{
					Name:   name,
					Method: zipCompressionMethod(name),
				})
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

		// Only finish the archive when every file made it. Closing the writer
		// after a failure would emit a central directory covering whatever was
		// written, handing the client a well-formed zip that's silently missing
		// files — and the 200 status went out before the first Telegram read, so
		// that's the only signal it would get. Leaving it unterminated makes the
		// download unambiguously broken instead.
		if err != nil {
			logger.Error("zip.stream_failed", zap.Error(err))
		} else if cerr := zw.Close(); cerr != nil {
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
	var result []struct {
		models.FileShare
		Type api.FileShareInfoType `gorm:"column:type"`
		Name string                `gorm:"column:name"`
	}

	notFoundErr := &apiError{err: errors.New("invalid share"), code: 404}
	if err := a.db.Model(&models.FileShare{}).Where("file_shares.file_id = ?", params.ID).Where("file_shares.user_id = ?", userId).
		Select("file_shares.*", "f.type", "f.name").
		Joins("left join teldrive.files as f on f.id = file_shares.file_id").
		Find(&result).Error; err != nil {
		if database.IsRecordNotFoundErr(err) {
			return nil, notFoundErr
		}
		return nil, &apiError{err: err}
	}

	if len(result) == 0 {
		return nil, notFoundErr
	}
	share := result[0]
	res := &api.FileShare{
		ID:   share.ID,
		Type: api.FileShareType(share.Type),
		Name: share.Name,
	}
	if share.Password != nil {
		res.Protected = true
	}
	if share.ExpiresAt != nil {
		res.ExpiresAt = api.NewOptDateTime(*share.ExpiresAt)
	}
	if share.ShortCode != nil {
		res.ShortCode = api.NewOptString(*share.ShortCode)
	}
	res.BlockDirectLink = api.NewOptBool(share.BlockDirectLink)
	res.AlwaysDirectLink = api.NewOptBool(share.AlwaysDirectLink)
	res.AllowZipDownload = api.NewOptBool(share.AllowZipDownload)
	return res, nil
}

func (a *apiService) FilesSuggestShareCode(ctx context.Context, params api.FilesSuggestShareCodeParams) (*api.ShortCodeSuggestion, error) {
	code, err := a.suggestUniqueShortCode(a.cnf.Shortlinks.CodeLength)
	if err != nil {
		return nil, &apiError{err: err}
	}
	return &api.ShortCodeSuggestion{Code: code}, nil
}

func (a *apiService) FilesUpdate(ctx context.Context, req *api.FileUpdate, params api.FilesUpdateParams) (*api.File, error) {

	userId := auth.GetUser(ctx)

	updateDb := models.File{}
	isContentUpdate := false
	uploadId := ""
	var uploads []models.Upload

	if req.UploadId.IsSet() && req.UploadId.Value != "" {
		uploadId = req.UploadId.Value
		// Scoped to the caller - see the matching lookup in FilesCreate.
		if err := a.db.Where("upload_id = ? AND user_id = ?", uploadId, userId).Order("part_no").Find(&uploads).Error; err != nil {
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
		// break the dedup link so this file becomes its own canonical copy. The link is
		// cleared separately below: referenced_file_id is a UUID column, so it can only
		// be released by writing NULL, and gorm's struct update has no way to express
		// that (a zero value is skipped, and a pointer to "" is not valid UUID input).
		clearReference := isContentUpdate && currentFile.ReferencedFileId != nil && *currentFile.ReferencedFileId != ""

		// Compute BLAKE3 tree hash if uploadId provided
		if blockHashesCover(uploads, req.Parts) {
			var allBlockHashes []byte
			for _, upload := range uploads {
				allBlockHashes = append(allBlockHashes, upload.BlockHashes...)
			}

			treeHash := hash.SumToHex(hash.ComputeTreeHash(allBlockHashes))
			updateDb.Hash = &treeHash

			// DEDUP CHECK: After updating content with new hash, check if it matches another file's hash
			// If it does, we might be re-deduplicating with a different file (less common but possible)
			existingFile, err := a.FindDeduplicateFile(ctx, currentFile.UserId, treeHash)
			if err != nil {
				logging.FromContext(ctx).Error("dedup check on update failed", zap.Error(err))
			}
			if existingFile != nil && existingFile.ID != currentFile.ID {
				// Found a duplicate with new hash: point this file to the canonical copy
				updateDb.ReferencedFileId = utils.Ptr(existingFile.ID)
				clearReference = false
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

		if clearReference {
			if err := database.RetryTransientLock(ctx, database.DefaultLockRetryAttempts, func() error {
				return tx.Model(&models.File{}).Where("id = ?", params.ID).
					Update("referenced_file_id", nil).Error
			}); err != nil {
				return err
			}
		}

		// Invalidate cached thumbnail when file content changes so the next
		// request regenerates it from the new bytes.
		if isContentUpdate {
			tx.Where("file_id = ?", params.ID).Delete(&models.Thumbnail{})
		}

		// Delete uploads after successful update
		if uploadId != "" {
			if err := tx.Where("upload_id = ? AND user_id = ?", uploadId, userId).Delete(&models.Upload{}).Error; err != nil {
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
		// Invalidate cached thumbnail when file content changes.
		a.db.Where("file_id = ?", params.ID).Delete(&models.Thumbnail{})
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

// cacheEntryReader serves part of a cached entry and closes it afterwards.
type cacheEntryReader struct {
	io.Reader
	entry *filecache.Entry
}

func (r *cacheEntryReader) Close() error { return r.entry.Close() }

// openCachedFile serves [start, start+length) straight out of the local file
// cache, so a file that is already cached never goes back to Telegram.
func (e *extendedService) openCachedFile(fileID string, start, length int64) (io.ReadCloser, bool) {
	if e.api.fileCache == nil || !e.api.fileCache.Enabled() || fileID == "" || length <= 0 {
		return nil, false
	}
	entry, ok := e.api.fileCache.Open(fileID)
	if !ok {
		return nil, false
	}
	if _, err := entry.Seek(start, io.SeekStart); err != nil {
		entry.Close()
		return nil, false
	}
	return &cacheEntryReader{Reader: io.LimitReader(entry, length), entry: entry}, true
}

// cacheTeeCloser mirrors everything read from src into the file cache. The entry
// is only published if the whole file arrives, so a client that disconnects early
// leaves nothing behind.
type cacheTeeCloser struct {
	io.Reader
	src   io.ReadCloser
	cache io.WriteCloser
}

func (c *cacheTeeCloser) Close() error {
	srcErr := c.src.Close()
	cacheErr := c.cache.Close()
	if srcErr != nil {
		return srcErr
	}
	return cacheErr
}

// edgeCacheable reports whether the request carries its credentials in the URL.
//
// Cloudflare's default cache key is the full URL including the query string, and
// it ignores Cookie and Authorization. So a response authenticated by cookie must
// never be marked cacheable: it would be stored under a key that says nothing
// about who asked, and then served to anyone who guesses the path. A URL that
// carries its own token (a share link's hash, an ephemeral download token) is
// safe, because only a client holding that token can name that cache entry.
func edgeCacheable(r *http.Request) bool {
	return r.URL.Query().Get("hash") != "" || r.URL.Query().Get("token") != ""
}

// setEdgeCache marks a response as edge-cacheable for seconds, or as private and
// uncacheable when seconds is zero. CDN-Cache-Control governs Cloudflare while
// Cache-Control still tells the browser what to do.
func setEdgeCache(w http.ResponseWriter, seconds int) {
	if seconds > 0 {
		value := fmt.Sprintf("public, max-age=%d, immutable", seconds)
		w.Header().Set("Cache-Control", value)
		w.Header().Set("CDN-Cache-Control", value)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("CDN-Cache-Control", "no-store")
}

// edgeCacheSeconds is how long a cacheable file response may be kept. Downloads
// and thumbnails are addressed by file ID and never change in place.
const edgeCacheSeconds = 86400

// streamEdgeCacheSeconds allows edge caching only for URL-tokenised requests.
func streamEdgeCacheSeconds(r *http.Request) int {
	if edgeCacheable(r) {
		return edgeCacheSeconds
	}
	return 0
}

func (e *extendedService) FilesStream(w http.ResponseWriter, r *http.Request, fileId string, userId int64) {
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	// Bound initialization only. Once the first bytes are ready, the stream may
	// run for as long as the caller remains connected.
	setupTimer := time.AfterFunc(15*time.Second, cancel)
	defer setupTimer.Stop()
	r = r.WithContext(ctx)
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
				writeStreamAuthError(w, err)
				return
			}
			userId, _ := strconv.ParseInt(user.Subject, 10, 64)
			session = &models.Session{UserId: userId, Session: user.TgSession}
		} else {
			session, err = auth.GetSessionByHash(ctx, e.api.db, e.api.cache, authHash)
			if err != nil {
				writeStreamAuthError(w, err)
				return
			}
		}
	} else {
		session = &models.Session{UserId: userId}
		owner, lookupErr := auth.GetSessionByUserId(ctx, e.api.db, e.api.cache, userId)
		if lookupErr != nil && !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
			writeStreamAuthError(w, lookupErr)
			return
		}
		if owner != nil {
			session.Session = owner.Session
		}
	}
	// Protect DB and Telegram setup separately from long-running stream bodies.
	finish, err := streamSetupAdmission.Begin(ctx, fmt.Sprintf("owner:%d", session.UserId))
	if err != nil {
		writeStreamError(w, err)
		return
	}
	setupFailed := false
	defer func() { finish(setupFailed) }()
	fileKey := fmt.Sprintf("file:%d:%s", session.UserId, fileId)
	finishFile, err := streamSetupAdmission.Begin(ctx, fileKey)
	if err != nil {
		writeStreamError(w, err)
		return
	}
	defer func() { finishFile(false) }()

	file, err := cache.Fetch(ctx, e.api.cache, cache.Key("files", fileId), 0, func() (*models.File, error) {
		var result models.File
		if err := e.api.db.WithContext(ctx).Model(&result).Where("id = ?", fileId).First(&result).Error; err != nil {
			return nil, err
		}
		return &result, nil
	})

	if err != nil {
		setupFailed = streamStatus(err) == 503
		writeStreamError(w, err)
		return
	}
	if err := file.ValidateStream(); err != nil {
		writeStreamError(w, err)
		return
	}

	w.Header().Set("Accept-Ranges", "bytes")

	var start, end int64

	rangeHeader := r.Header.Get("Range")
	contentType := contentTypeFor(file.Name, file.MimeType)

	// PSD-to-PNG conversion: when format=png is requested for a PSD file,
	// download the entire file, convert off-thread, and serve the PNG.
	if r.URL.Query().Get("format") == "png" && psdconv.IsPSD(contentType, file.Name) {
		e.servePSDAsPNG(w, r, file, session, logger)
		return
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
	prepare := func() {
		setupTimer.Stop()
		finish(false)
		finishFile(false)
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Content-Length", strconv.FormatInt(contentLength, 10))
		w.Header().Set("ETag", fmt.Sprintf("\"%s\"", md5.FromString(fileId+strconv.FormatInt(*file.Size, 10))))
		if file.UpdatedAt != nil {
			w.Header().Set("Last-Modified", file.UpdatedAt.UTC().Format(http.TimeFormat))
		}
		disposition := "inline"
		if r.URL.Query().Get("download") == "1" {
			disposition = "attachment"
		}
		w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": file.Name}))
		// Only a URL carrying its own token may be edge-cached. A cookie session
		// must never be stored under a key that ignores the cookie.
		setEdgeCache(w, streamEdgeCacheSeconds(r))
	}
	if r.Method == http.MethodHead {
		prepare()
		w.WriteHeader(status)
		return
	}

	tokens, err := e.api.channelManager.BotTokens(ctx, session.UserId)
	if err != nil {
		setupFailed = true
		writeStreamError(w, err)
		return
	}
	if limit := e.api.cnf.TG.Stream.BotsLimit; limit > 0 && len(tokens) > limit {
		tokens = tokens[:limit]
	}
	var client *telegram.Client
	var token string
	// Streaming setup is already admission-limited. Do not layer reconnect,
	// flood-wait, or five-attempt recovery loops onto every mount request.
	middlewares := tgc.NewMiddleware(&e.api.cnf.TG, tgc.WithRetry(1), tgc.WithRateLimit())
	if len(tokens) == 0 {
		if session.Session == "" {
			http.Error(w, "Telegram session unavailable; sign in again", http.StatusForbidden)
			return
		}
		client, err = tgc.AuthClient(ctx, &e.api.cnf.TG, session.Session, middlewares...)
	} else {
		token, _, err = e.api.botSelector.Next(ctx, tgc.BotOpStream, session.UserId, tokens)
		if err == nil {
			client, err = tgc.BotClient(ctx, e.api.db, e.api.cache, &e.api.cnf.TG, token, middlewares...)
		}
	}
	if err != nil {
		setupFailed = streamStatus(err) == 503
		logger.Error("stream.client_failed", zap.Error(err))
		writeStreamError(w, err)
		return
	}
	botID := strconv.FormatInt(session.UserId, 10)
	if token != "" {
		botID = strings.Split(token, ":")[0]
	}
	committed, err := writeStream(ctx, w, status, contentLength,
		func(ctx context.Context, f func(context.Context) error) error {
			return tgc.RunWithAuth(ctx, client, token, f)
		},
		func(ctx context.Context) (io.ReadCloser, error) {
			// Already cached locally: serve the range without touching Telegram.
			if cached, ok := e.openCachedFile(fileId, start, contentLength); ok {
				return cached, nil
			}
			parts, err := getParts(ctx, client, e.api.cache, file)
			if err != nil {
				return nil, err
			}
			lr, err := reader.NewReader(ctx, client.API(), e.api.cache, file, parts, start, end, &e.api.cnf.TG, botID)
			if err != nil {
				return nil, err
			}
			// Only a whole-file read can populate the cache: a partial read would
			// store a truncated entry.
			if start != 0 || end != *file.Size-1 || e.api.fileCache == nil || !e.api.fileCache.Enabled() {
				return lr, nil
			}
			cacheWriter, err := e.api.fileCache.Store(fileId, *file.Size)
			if err != nil {
				// ErrSkipped (too large) and friends: just stream it.
				return lr, nil
			}
			return &cacheTeeCloser{Reader: io.TeeReader(lr, cacheWriter), src: lr, cache: cacheWriter}, nil
		}, prepare, logger)
	if err != nil {
		logger.Error("stream.failed", zap.Error(err), zap.Bool("committed", committed))
		setupFailed = streamStatus(err) == 503 && !errors.Is(err, context.Canceled)
		if committed {
			if setupFailed {
				streamSetupAdmission.Cooldown(fileKey)
			}
			// Abort on the HTTP goroutine, not inside gotd. net/http resets HTTP/2
			// streams or closes HTTP/1 connections without appending plaintext media.
			panic(http.ErrAbortHandler)
		}
		writeStreamError(w, err)
	}
}

// servePSDAsPNG downloads the full PSD, converts it to PNG off the request
// goroutine, and writes the result. Called from FilesStream when ?format=png
// is requested for a PSD file.
func (e *extendedService) servePSDAsPNG(w http.ResponseWriter, r *http.Request, file *models.File, session *models.Session, logger *zap.Logger) {
	ctx := r.Context()

	if file.Size != nil && *file.Size > 200*1024*1024 {
		http.Error(w, "PSD too large for conversion (max 200 MB)", http.StatusRequestedRangeNotSatisfiable)
		return
	}

	tokens, err := e.api.channelManager.BotTokens(ctx, session.UserId)
	if err != nil {
		logger.Error("psd.bots_fetch_failed", zap.Error(err))
		http.Error(w, "failed to get bots", http.StatusInternalServerError)
		return
	}
	if limit := e.api.cnf.TG.Stream.BotsLimit; limit > 0 && len(tokens) > limit {
		tokens = tokens[:limit]
	}

	var (
		client *telegram.Client
		token  string
	)
	if len(tokens) == 0 {
		client, err = tgc.AuthClient(ctx, &e.api.cnf.TG, session.Session, e.api.newMiddlewares(ctx, 5)...)
	} else {
		token, _, err = e.api.botSelector.Next(ctx, tgc.BotOpStream, session.UserId, tokens)
		if err == nil {
			client, err = tgc.BotClient(ctx, e.api.db, e.api.cache, &e.api.cnf.TG, token, e.api.newMiddlewares(ctx, 5)...)
		}
	}
	if err != nil {
		logger.Error("psd.client_failed", zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	botID := strconv.FormatInt(session.UserId, 10)
	if token != "" {
		if parts := strings.Split(token, ":"); len(parts) > 0 {
			botID = parts[0]
		}
	}

	// Download the full PSD and convert in a separate goroutine, piping the
	// result back to the response writer through a channel.
	type result struct {
		buf *bytes.Buffer
		err error
	}
	ch := make(chan result, 1)

	tgc.RunWithAuth(ctx, client, token, func(ctx context.Context) error {
		fileParts, err := getParts(ctx, client, e.api.cache, file)
		if err != nil {
			ch <- result{nil, err}
			return nil
		}

		lr, err := reader.NewReader(ctx, client.API(), e.api.cache, file, fileParts, 0, *file.Size-1, &e.api.cnf.TG, botID)
		if err != nil {
			ch <- result{nil, err}
			return nil
		}
		defer lr.Close()

		go func() {
			buf, convErr := psdconv.ToPNG(ctx, lr)
			ch <- result{buf, convErr}
		}()
		return nil
	})

	res := <-ch
	if res.err != nil {
		logger.Error("psd.convert_failed", zap.Error(res.err))
		http.Error(w, "PSD conversion failed: "+res.err.Error(), http.StatusInternalServerError)
		return
	}

	pngName := strings.TrimSuffix(file.Name, filepath.Ext(file.Name)) + ".png"
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Length", strconv.Itoa(res.buf.Len()))
	w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": pngName}))
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.WriteHeader(http.StatusOK)
	io.Copy(w, res.buf)
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
