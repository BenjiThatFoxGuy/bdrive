package services

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
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
	"github.com/tgdrive/teldrive/internal/thumbnail"
	"github.com/tgdrive/teldrive/internal/tgc"
	"github.com/tgdrive/teldrive/pkg/models"
	"github.com/tgdrive/teldrive/pkg/types"
	"go.uber.org/zap"
)

// thumbGenerator is shared across requests so the concurrency semaphore works.
var thumbGenerator = thumbnail.NewGenerator(500*1024*1024, 30*time.Second, 2)

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

// serveCachedThumb downloads a previously-generated thumbnail from Telegram and
// serves it through the Source handler (Range + HEAD support for imgproxy).
func (e *extendedService) serveCachedThumb(w http.ResponseWriter, r *http.Request, thumb *models.Thumbnail, session *models.Session, logger *zap.Logger) {
	ctx := r.Context()

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

	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("ETag", `"`+thumb.FileId+`"`)
	w.Header().Set("Content-Type", "image/jpeg")
	thumbnail.NewSource(int64(len(data)), func(_ context.Context, start, end int64) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(data[start : end+1])), nil
	}).ServeHTTP(w, r)
}

// generateThumb downloads the video, runs ffmpeg, caches the result in
// Telegram, persists the DB record, and serves the JPEG.
func (e *extendedService) generateThumb(w http.ResponseWriter, r *http.Request, file *models.File, session *models.Session, logger *zap.Logger) {
	ctx := r.Context()

	if file.Size == nil || *file.Size == 0 {
		http.Error(w, "empty file", http.StatusBadRequest)
		return
	}

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

	tgc.RunWithAuth(ctx, client, token, func(ctx context.Context) error {
		go func() {
			// Download full video.
			fileParts, err := getParts(ctx, client, e.api.cache, file)
			if err != nil {
				ch <- result{err: err}
				return
			}

			lr, err := reader.NewReader(ctx, client.API(), e.api.cache, file, fileParts, 0, *file.Size-1, &e.api.cnf.TG, botID)
			if err != nil {
				ch <- result{err: err}
				return
			}
			defer lr.Close()

			// Generate JPEG thumbnail via ffmpeg.
			jpegBytes, err := thumbGenerator.Generate(ctx, lr, *file.Size)
			if err != nil {
				ch <- result{err: err}
				return
			}

			// Prepare upload data (encrypt if original is encrypted).
			uploadData := jpegBytes
			var salt string
			if file.IsEncrypted() {
				saltBytes := make([]byte, 16)
				if _, err := rand.Read(saltBytes); err != nil {
					ch <- result{err: err}
					return
				}
				salt = base64.StdEncoding.EncodeToString(saltBytes)
				cipher, err := crypt.NewCipher(e.api.cnf.TG.Uploads.EncryptionKey, salt)
				if err != nil {
					ch <- result{err: err}
					return
				}
				enc, err := cipher.EncryptData(bytes.NewReader(jpegBytes))
				if err != nil {
					ch <- result{err: err}
					return
				}
				uploadData, err = io.ReadAll(enc)
				if err != nil {
					ch <- result{err: err}
					return
				}
			}

			// Upload thumbnail to same channel as original.
			msgID, err := uploadThumbToChannel(ctx, client.API(), *file.ChannelId, uploadData, e.api.cnf.TG.Uploads.Threads)
			if err != nil {
				ch <- result{err: err}
				return
			}

			// Persist to DB.
			thumb := models.Thumbnail{
				FileId:    file.ID,
				UserId:    session.UserId,
				ChannelId: *file.ChannelId,
				MessageId: msgID,
				Encrypted: file.IsEncrypted(),
				Salt:      salt,
				Size:      int64(len(uploadData)),
				MimeType:  "image/jpeg",
			}
			if dbErr := e.api.db.Create(&thumb).Error; dbErr != nil {
				logger.Error("thumb.db_save_failed", zap.Error(dbErr))
			}

			ch <- result{jpeg: jpegBytes, msgID: msgID}
		}()
		return nil
	})

	res := <-ch
	if res.err != nil {
		logger.Error("thumb.generate_failed", zap.Error(res.err))
		http.Error(w, "thumbnail generation failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("ETag", `"`+file.ID+`"`)
	w.Header().Set("Content-Type", "image/jpeg")
	thumbnail.NewSource(int64(len(res.jpeg)), func(_ context.Context, start, end int64) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(res.jpeg[start : end+1])), nil
	}).ServeHTTP(w, r)
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

// uploadThumbToChannel uploads a small blob to a Telegram channel and returns
// the message ID of the resulting document.
func uploadThumbToChannel(ctx context.Context, client *tg.Client, channelId int64, data []byte, threads int) (int, error) {
	channel, err := tgc.GetChannelById(ctx, client, channelId)
	if err != nil {
		return 0, err
	}
	u := uploader.NewUploader(client).WithThreads(threads).WithPartSize(512 * 1024)
	upload, err := u.Upload(ctx, uploader.NewUpload("thumb.jpg", bytes.NewReader(data), int64(len(data))))
	if err != nil {
		return 0, err
	}
	doc := message.UploadedDocument(upload).Filename("thumb.jpg").ForceFile(true)
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
