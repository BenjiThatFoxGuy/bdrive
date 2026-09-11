package services

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gotd/td/telegram"
	"go.uber.org/zap"

	"github.com/tgdrive/teldrive/internal/auth"
	"github.com/tgdrive/teldrive/internal/cache"
	"github.com/tgdrive/teldrive/internal/logging"
	"github.com/tgdrive/teldrive/internal/tgc"
	"github.com/tgdrive/teldrive/internal/zipbrowse"
	"github.com/tgdrive/teldrive/pkg/models"
	"github.com/tgdrive/teldrive/internal/reader"
)

// ZipBrowse lists the contents of a zip file at a given inner path.
// GET /files/zip/{fileId}/list?path=/inner/path
func (e *extendedService) ZipBrowse(w http.ResponseWriter, r *http.Request, fileId string) {
	if !e.api.cnf.Files.EnableZipBrowsing {
		http.Error(w, "zip browsing is disabled", http.StatusForbidden)
		return
	}

	ctx := r.Context()
	logger := logging.Component("ZIP").With(zap.String("file_id", fileId))

	session, userId, err := e.resolveZipSession(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	file, err := e.getFileForZip(ctx, fileId, userId)
	if err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}

	if file.Size == nil || *file.Size == 0 {
		zipbrowse.WriteJSON(w, http.StatusOK, &zipbrowse.ListResponse{Entries: []zipbrowse.Entry{}, Total: 0})
		return
	}

	if *file.Size > 500*1024*1024 {
		http.Error(w, "zip too large for browsing (max 500 MB)", http.StatusRequestedRangeNotSatisfiable)
		return
	}

	innerPath := r.URL.Query().Get("path")

	buf, err := e.downloadFullFile(ctx, file, session, logger)
	if err != nil {
		logger.Error("zip.download_failed", zap.Error(err))
		http.Error(w, "failed to download zip", http.StatusInternalServerError)
		return
	}

	rdr := bytes.NewReader(buf.Bytes())
	listing, err := zipbrowse.ListEntries(rdr, int64(buf.Len()), innerPath)
	if err != nil {
		logger.Error("zip.list_failed", zap.Error(err))
		http.Error(w, "failed to read zip: "+err.Error(), http.StatusInternalServerError)
		return
	}

	zipbrowse.WriteJSON(w, http.StatusOK, listing)
}

// ZipExtract serves a single file from inside a zip archive.
// GET /files/zip/{fileId}/file?path=/inner/path/to/file.txt
func (e *extendedService) ZipExtract(w http.ResponseWriter, r *http.Request, fileId string) {
	if !e.api.cnf.Files.EnableZipBrowsing {
		http.Error(w, "zip browsing is disabled", http.StatusForbidden)
		return
	}

	ctx := r.Context()
	logger := logging.Component("ZIP").With(zap.String("file_id", fileId))

	session, userId, err := e.resolveZipSession(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	file, err := e.getFileForZip(ctx, fileId, userId)
	if err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}

	if file.Size == nil || *file.Size == 0 {
		http.Error(w, "empty file", http.StatusNotFound)
		return
	}

	if *file.Size > 500*1024*1024 {
		http.Error(w, "zip too large for extraction (max 500 MB)", http.StatusRequestedRangeNotSatisfiable)
		return
	}

	innerPath := r.URL.Query().Get("path")
	if innerPath == "" {
		http.Error(w, "path parameter required", http.StatusBadRequest)
		return
	}

	buf, err := e.downloadFullFile(ctx, file, session, logger)
	if err != nil {
		logger.Error("zip.download_failed", zap.Error(err))
		http.Error(w, "failed to download zip", http.StatusInternalServerError)
		return
	}

	rdr := bytes.NewReader(buf.Bytes())
	if err := zipbrowse.ExtractFile(w, rdr, int64(buf.Len()), innerPath); err != nil {
		logger.Error("zip.extract_failed", zap.Error(err))
		http.Error(w, err.Error(), http.StatusNotFound)
	}
}

// resolveZipSession extracts the user session from the request.
func (e *extendedService) resolveZipSession(r *http.Request) (*models.Session, int64, error) {
	ctx := r.Context()

	cookie, err := r.Cookie(authCookieName)
	if err != nil {
		authHash := r.URL.Query().Get("hash")
		if authHash == "" {
			return nil, 0, fmt.Errorf("missing credentials")
		}
		session, err := auth.GetSessionByHash(ctx, e.api.db, e.api.cache, authHash)
		if err != nil {
			return nil, 0, fmt.Errorf("invalid hash")
		}
		return session, session.UserId, nil
	}

	user, err := auth.VerifyUser(ctx, e.api.db, e.api.cache, e.api.cnf.JWT.Secret, cookie.Value)
	if err != nil {
		return nil, 0, fmt.Errorf("invalid token")
	}
	userId, _ := strconv.ParseInt(user.Subject, 10, 64)
	session := &models.Session{UserId: userId, Session: user.TgSession}
	return session, userId, nil
}

// getFileForZip fetches a file record by ID, verifying ownership.
func (e *extendedService) getFileForZip(ctx context.Context, fileId string, userId int64) (*models.File, error) {
	file, err := cache.Fetch(ctx, e.api.cache, cache.Key("files", fileId), 0, func() (*models.File, error) {
		var result models.File
		if err := e.api.db.Model(&result).Where("id = ? AND user_id = ?", fileId, userId).First(&result).Error; err != nil {
			return nil, err
		}
		return &result, nil
	})
	return file, err
}

// downloadFullFile downloads the entire file into memory via the TG client.
func (e *extendedService) downloadFullFile(ctx context.Context, file *models.File, session *models.Session, logger *zap.Logger) (*bytes.Buffer, error) {
	tokens, err := e.api.channelManager.BotTokens(ctx, session.UserId)
	if err != nil {
		return nil, fmt.Errorf("fetch bots: %w", err)
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
		return nil, fmt.Errorf("client init: %w", err)
	}

	botID := strconv.FormatInt(session.UserId, 10)
	if token != "" {
		if parts := strings.Split(token, ":"); len(parts) > 0 {
			botID = parts[0]
		}
	}

	type dlResult struct {
		buf *bytes.Buffer
		err error
	}
	ch := make(chan dlResult, 1)

	tgc.RunWithAuth(ctx, client, token, func(ctx context.Context) error {
		fileParts, err := getParts(ctx, client, e.api.cache, file)
		if err != nil {
			ch <- dlResult{nil, err}
			return nil
		}

		lr, err := reader.NewReader(ctx, client.API(), e.api.cache, file, fileParts, 0, *file.Size-1, &e.api.cnf.TG, botID)
		if err != nil {
			ch <- dlResult{nil, err}
			return nil
		}

		go func() {
			defer lr.Close()
			var buf bytes.Buffer
			_, copyErr := io.Copy(&buf, lr)
			if copyErr != nil {
				ch <- dlResult{nil, copyErr}
			} else {
				ch <- dlResult{&buf, nil}
			}
		}()
		return nil
	})

	res := <-ch
	return res.buf, res.err
}
