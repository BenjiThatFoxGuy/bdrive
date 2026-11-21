package services

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
	"github.com/tgdrive/teldrive/internal/auth"
	"github.com/tgdrive/teldrive/internal/cache"
	"github.com/tgdrive/teldrive/internal/crypt"
	"github.com/tgdrive/teldrive/internal/logging"
	"github.com/tgdrive/teldrive/internal/reader"
	"github.com/tgdrive/teldrive/internal/tgc"
	"github.com/tgdrive/teldrive/internal/thumbnail"
	"github.com/tgdrive/teldrive/pkg/models"
	"github.com/tgdrive/teldrive/pkg/types"
	"go.uber.org/zap"
)

// thumbGenerator is shared across requests so the concurrency semaphore works.
var thumbGenerator = thumbnail.NewGenerator(0, 2*time.Minute, 2)

// FileThumbnail serves (or generates on-demand) a JPEG thumbnail for a video
// file. imgproxy fetches this endpoint and resizes the result for the UI.
func (e *extendedService) FileThumbnail(w http.ResponseWriter, r *http.Request, fileId string) {
	ctx := r.Context()
	logger := logging.Component("THUMB").With(zap.String("file_id", fileId))

	// --- Auth (same pattern as FilesStream) ---
	var session *models.Session

	authHash := r.URL.Query().Get("hash")
	if authHash == "" {
		cookie, err := r.Cookie(authCookieName)
		if err != nil {
			http.Error(w, "missing token or hash", http.StatusUnauthorized)
			return
		}
		user, err := auth.VerifyUser(ctx, e.api.db, e.api.cache, e.api.cnf.JWT.Secret, cookie.Value)
		if err != nil {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}
		userId, _ := strconv.ParseInt(user.Subject, 10, 64)
		session = &models.Session{UserId: userId, Session: user.TgSession}
	} else {
		var err error
		session, err = auth.GetSessionByHash(ctx, e.api.db, e.api.cache, authHash)
		if err != nil {
			http.Error(w, "invalid hash", http.StatusBadRequest)
			return
		}
	}

	// --- Load file ---
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

	// --- Must be a video ---
	if file.Category == nil || *file.Category != "video" {
		http.Error(w, "not a video file", http.StatusBadRequest)
		return
	}

	// --- Check for cached thumbnail ---
	var thumb models.Thumbnail
	if err := e.api.db.Where("file_id = ?", fileId).First(&thumb).Error; err == nil {
		e.serveCachedThumb(w, r, &thumb, session, logger)
		return
	}

	// --- Generate, cache, and serve ---
	e.generateThumb(w, r, file, session, logger)
}

// openForThumbnail returns a reader over the whole decrypted file, preferring
// the local cache so a repeat thumbnail does not re-download the video.
func (e *extendedService) openForThumbnail(ctx context.Context, client *telegram.Client, file *models.File, botID string) (io.Reader, io.Closer, error) {
	if cached, ok := e.openCachedFile(file.ID, 0, *file.Size); ok {
		return cached, cached, nil
	}
	fileParts, err := getParts(ctx, client, e.api.cache, file)
	if err != nil {
		return nil, nil, err
	}
	lr, err := reader.NewReader(ctx, client.API(), e.api.cache, file, fileParts, 0, *file.Size-1, &e.api.cnf.TG, botID)
	if err != nil {
		return nil, nil, err
	}
	return lr, lr, nil
}

// serveCachedThumb downloads a previously-generated thumbnail from Telegram and
// serves it through the Source handler (Range + HEAD support for imgproxy).
func (e *extendedService) serveCachedThumb(w http.ResponseWriter, r *http.Request, thumb *models.Thumbnail, session *models.Session, logger *zap.Logger) {
	ctx := r.Context()
	cacheKey := "thumb:" + thumb.FileId

	serve := func(data []byte) {
		setEdgeCache(w, streamEdgeCacheSeconds(r))
		w.Header().Set("ETag", `"`+thumb.FileId+`"`)
		w.Header().Set("Content-Type", "image/jpeg")
		thumbnail.NewSource(int64(len(data)), func(_ context.Context, start, end int64) (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(data[start : end+1])), nil
		}).ServeHTTP(w, r)
	}

	// A thumbnail already generated once is kept locally, so imgproxy and repeat
	// views do not download it from Telegram again.
	if e.api.fileCache != nil && e.api.fileCache.Enabled() {
		if entry, ok := e.api.fileCache.Open(cacheKey); ok {
			data, err := io.ReadAll(entry)
			entry.Close()
			if err == nil {
				serve(data)
				return
			}
			logger.Warn("thumb.cache_read_failed", zap.Error(err))
		}
	}

	client, token, botID, err := e.resolveStreamClient(ctx, session)
	if err != nil {
		logger.Error("thumb.client_failed", zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var data []byte
	var dlErr error

	tgc.RunWithAuth(ctx, client, token, func(ctx context.Context) error {
		enc := thumb.Encrypted
		chId := thumb.ChannelId
		syntheticFile := &models.File{
			ID:        thumb.ID,
			ChannelId: &chId,
			Encrypted: &enc,
		}

		parts := []types.Part{{
			ID:   int64(thumb.MessageId),
			Size: thumb.Size,
			Salt: thumb.Salt,
		}}
		readSize := thumb.Size
		if thumb.Encrypted {
			dec, _ := crypt.DecryptedSize(thumb.Size)
			parts[0].DecryptedSize = dec
			readSize = dec
		}

		lr, err := reader.NewReader(ctx, client.API(), e.api.cache, syntheticFile, parts, 0, readSize-1, &e.api.cnf.TG, botID)
		if err != nil {
			dlErr = err
			return nil
		}
		defer lr.Close()
		data, dlErr = io.ReadAll(lr)
		return nil
	})

	if dlErr != nil {
		logger.Error("thumb.download_failed", zap.Error(dlErr))
		http.Error(w, "failed to fetch thumbnail", http.StatusInternalServerError)
		return
	}

	if e.api.fileCache != nil && e.api.fileCache.Enabled() {
		if cw, err := e.api.fileCache.Store(cacheKey, int64(len(data))); err == nil {
			_, werr := cw.Write(data)
			if werr == nil {
				werr = cw.Close()
			}
			if werr != nil {
				logger.Warn("thumb.cache_store_failed", zap.Error(werr))
			}
		}
	}

	serve(data)
}

// generateThumb generates a JPEG thumbnail for a video file. For unencrypted
// files it gives ffmpeg the public stream URL directly, so the whole Telegram
// download pipeline is bypassed. Encrypted files still need the full MTProto
// download because ffmpeg cannot decrypt them.
func (e *extendedService) generateThumb(w http.ResponseWriter, r *http.Request, file *models.File, session *models.Session, logger *zap.Logger) {
	ctx := r.Context()

	if file.Size == nil || *file.Size == 0 {
		http.Error(w, "empty file", http.StatusBadRequest)
		return
	}

	// --- URL path: ffmpeg reads the public stream URL directly ---
	// The stream endpoint decrypts on the fly, so this works for both encrypted
	// and unencrypted files.
	if e.api.cnf.Server.BaseURL != "" {
		streamURL := e.buildStreamURL(file, session)
		logger.Debug("thumb.url_mode", zap.String("url", streamURL))

		jpegBytes, err := thumbGenerator.GenerateFromURL(ctx, streamURL)
		if err != nil {
			logger.Warn("thumb.url_failed, falling back to mtproto", zap.Error(err))
			// Fall through to the MTProto path below.
		} else {
			e.finishThumb(ctx, w, r, file, session, jpegBytes, logger)
			return
		}
	}

	// --- MTProto path (encrypted files or URL fallback) ---

	client, token, botID, err := e.resolveStreamClient(ctx, session)
	if err != nil {
		logger.Error("thumb.client_failed", zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	type result struct {
		jpeg  []byte
		msgID int
		err   error
	}
	ch := make(chan result, 1)

	// The goroutine runs inside the RunWithAuth callback so the gotd client stays
	// alive for the entire download+ffmpeg+upload. done is closed when the
	// goroutine finishes, which lets the callback return and disconnect the client.
	// The result travels through ch (buffered 1) to the HTTP handler below.
	done := make(chan struct{})
	tgc.RunWithAuth(ctx, client, token, func(ctx context.Context) error {
		go func() {
			defer close(done)

			src, closeSrc, err := e.openForThumbnail(ctx, client, file, botID)
			if err != nil {
				ch <- result{err: err}
				return
			}
			defer closeSrc.Close()

			jpegBytes, err := thumbGenerator.Generate(ctx, src, *file.Size)
			if err != nil {
				ch <- result{err: err}
				return
			}

			ch <- result{jpeg: jpegBytes}
		}()

		// Keep the client alive until the goroutine finishes.
		<-done
		return nil
	})

	res := <-ch
	if res.err != nil {
		logger.Error("thumb.generate_failed", zap.Error(res.err))
		http.Error(w, "thumbnail generation failed", http.StatusInternalServerError)
		return
	}

	e.finishThumb(ctx, w, r, file, session, res.jpeg, logger)
}

// resolveStreamClient creates a Telegram client for streaming/downloading,
// following the same bot-resolution pattern as FilesStream.
func (e *extendedService) resolveStreamClient(ctx context.Context, session *models.Session) (*telegram.Client, string, string, error) {
	tokens, err := e.api.channelManager.BotTokens(ctx, session.UserId)
	if err != nil {
		return nil, "", "", err
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
		return nil, "", "", err
	}

	botID := strconv.FormatInt(session.UserId, 10)
	if token != "" {
		if parts := strings.Split(token, ":"); len(parts) > 0 {
			botID = parts[0]
		}
	}
	return client, token, botID, nil
}

// buildStreamURL constructs the public ephemeral URL for a file, the same one
// the browser gets from "copy link". It uses the session hash for auth so the
// stream endpoint authenticates without cookies.
func (e *extendedService) buildStreamURL(file *models.File, session *models.Session) string {
	base := strings.TrimRight(e.api.cnf.Server.BaseURL, "/")
	u := fmt.Sprintf("%s/api/files/%s/%s?hash=%s",
		base,
		file.ID,
		file.Name,
		session.Hash,
	)
	return u
}

// finishThumb uploads the generated JPEG to Telegram, persists the DB record,
// caches the bytes locally, and serves the response. Shared between the URL
// path and the MTProto path.
func (e *extendedService) finishThumb(ctx context.Context, w http.ResponseWriter, r *http.Request, file *models.File, session *models.Session, jpegBytes []byte, logger *zap.Logger) {
	uploadData := jpegBytes
	var salt string

	if file.IsEncrypted() {
		saltBytes := make([]byte, 16)
		if _, err := rand.Read(saltBytes); err != nil {
			logger.Error("thumb.encrypt_failed", zap.Error(err))
			http.Error(w, "thumbnail encryption failed", http.StatusInternalServerError)
			return
		}
		salt = base64.StdEncoding.EncodeToString(saltBytes)
		cipher, err := crypt.NewCipher(e.api.cnf.TG.Uploads.EncryptionKey, salt)
		if err != nil {
			logger.Error("thumb.cipher_failed", zap.Error(err))
			http.Error(w, "thumbnail encryption failed", http.StatusInternalServerError)
			return
		}
		enc, err := cipher.EncryptData(bytes.NewReader(jpegBytes))
		if err != nil {
			logger.Error("thumb.encrypt_data_failed", zap.Error(err))
			http.Error(w, "thumbnail encryption failed", http.StatusInternalServerError)
			return
		}
		var encErr error
		uploadData, encErr = io.ReadAll(enc)
		if encErr != nil {
			logger.Error("thumb.encrypt_read_failed", zap.Error(encErr))
			http.Error(w, "thumbnail encryption failed", http.StatusInternalServerError)
			return
		}
	}

	// Upload to the same channel - needs a gotd client for the API call.
	client, token, _, err := e.resolveStreamClient(ctx, session)
	if err != nil {
		logger.Error("thumb.upload_client_failed", zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var msgID int
	thumbChannelId := *file.ChannelId
	tgc.RunWithAuth(ctx, client, token, func(ctx context.Context) error {
		var uploadErr error
		msgID, uploadErr = uploadThumbToChannel(ctx, client.API(), thumbChannelId, uploadData, e.api.cnf.TG.Uploads.Threads, file.Name)
		if uploadErr != nil {
			logger.Warn("thumb.upload_failed_trying_default", zap.Error(uploadErr))
			defaultCh, chErr := e.api.channelManager.GetChannel(ctx, session.UserId)
			if chErr != nil || defaultCh == thumbChannelId {
				return uploadErr
			}
			msgID, uploadErr = uploadThumbToChannel(ctx, client.API(), defaultCh, uploadData, e.api.cnf.TG.Uploads.Threads, file.Name)
			if uploadErr != nil {
				return uploadErr
			}
			thumbChannelId = defaultCh
		}
		return nil
	})

	if msgID == 0 {
		logger.Error("thumb.upload_failed")
		http.Error(w, "thumbnail upload failed", http.StatusInternalServerError)
		return
	}

	thumb := models.Thumbnail{
		FileId:    file.ID,
		UserId:    session.UserId,
		ChannelId: thumbChannelId,
		MessageId: msgID,
		Encrypted: file.IsEncrypted(),
		Salt:      salt,
		Size:      int64(len(uploadData)),
		MimeType:  "image/jpeg",
	}
	if dbErr := e.api.db.Create(&thumb).Error; dbErr != nil {
		logger.Error("thumb.db_save_failed", zap.Error(dbErr))
	}

	// Cache locally.
	if e.api.fileCache != nil && e.api.fileCache.Enabled() {
		cacheKey := "thumb:" + file.ID
		if cw, cwErr := e.api.fileCache.Store(cacheKey, int64(len(jpegBytes))); cwErr == nil {
			cw.Write(jpegBytes)
			cw.Close()
		}
	}

	setEdgeCache(w, streamEdgeCacheSeconds(r))
	w.Header().Set("ETag", `"`+file.ID+`"`)
	w.Header().Set("Content-Type", "image/jpeg")
	thumbnail.NewSource(int64(len(jpegBytes)), func(_ context.Context, start, end int64) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(jpegBytes[start : end+1])), nil
	}).ServeHTTP(w, r)
}

// uploadThumbToChannel uploads a small blob to a Telegram channel and returns
// the message ID of the resulting document.
func uploadThumbToChannel(ctx context.Context, client *tg.Client, channelId int64, data []byte, threads int, originalName string) (int, error) {
	thumbName := strings.TrimSuffix(originalName, filepath.Ext(originalName)) + "_thumb.jpg"
	channel, err := tgc.GetChannelById(ctx, client, channelId)
	if err != nil {
		return 0, err
	}
	u := uploader.NewUploader(client).WithThreads(threads).WithPartSize(512 * 1024)
	upload, err := u.Upload(ctx, uploader.NewUpload(thumbName, bytes.NewReader(data), int64(len(data))))
	if err != nil {
		return 0, err
	}
	doc := message.UploadedDocument(upload).Filename(thumbName).ForceFile(true)
	sender := message.NewSender(client)
	target := sender.To(&tg.InputPeerChannel{ChannelID: channel.ChannelID, AccessHash: channel.AccessHash})
	res, err := target.Media(ctx, doc)
	if err != nil {
		return 0, err
	}
	updates, ok := res.(*tg.Updates)
	if !ok {
		return 0, fmt.Errorf("upload: unexpected response type %T", res)
	}
	for _, update := range updates.Updates {
		if cm, ok := update.(*tg.UpdateNewChannelMessage); ok {
			if msg, ok := cm.Message.AsNotEmpty(); ok {
				if m, ok := msg.(*tg.Message); ok {
					return m.ID, nil
				}
			}
		}
	}
	return 0, fmt.Errorf("upload failed: no message in response")
}
