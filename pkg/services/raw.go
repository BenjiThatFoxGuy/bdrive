package services

import (
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
	"github.com/tgdrive/teldrive/internal/api"
	"github.com/tgdrive/teldrive/internal/auth"
	"github.com/tgdrive/teldrive/internal/cache"
	jetmodel "github.com/tgdrive/teldrive/internal/database/jet/gen/model"
	"github.com/tgdrive/teldrive/internal/events"
	"github.com/tgdrive/teldrive/internal/http_range"
	"github.com/tgdrive/teldrive/internal/logging"
	"github.com/tgdrive/teldrive/internal/md5"
	"github.com/tgdrive/teldrive/internal/reader"
	"github.com/tgdrive/teldrive/pkg/dto"
	"github.com/tgdrive/teldrive/pkg/mapper"
	"github.com/tgdrive/teldrive/pkg/repositories"
	"github.com/tgdrive/teldrive/pkg/types"
	varc "github.com/tgdrive/varc/varc"
	"go.uber.org/zap"
)

type rawService struct {
	api *apiService
}

func NewRawService(api *apiService) *rawService {
	return &rawService{api: api}
}

func (s *rawService) EventsEventsStream(ctx context.Context, params api.EventsEventsStreamParams, w http.ResponseWriter) error {
	flusher, ok := responseFlusher(w)
	if !ok {
		return &apiError{err: fmt.Errorf("streaming not supported")}
	}
	eventTypes, err := events.ParseEventTypes(params.Types)
	if err != nil {
		return &apiError{err: err, code: http.StatusBadRequest}
	}
	interval := 30 * time.Second
	if v, ok := params.Interval.Get(); ok && v > 0 {
		interval = time.Duration(v) * time.Millisecond
	}
	userID := auth.User(ctx)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	eventChan := s.api.events.Subscribe(userID, eventTypes)
	defer s.api.events.Unsubscribe(userID, eventChan)
	lastSeq := int64(0)
	if rawLastEventID := params.LastEventID.Or(""); rawLastEventID != "" {
		if parsed, parseErr := strconv.ParseInt(rawLastEventID, 10, 64); parseErr == nil && parsed > 0 {
			lastSeq = parsed
		}
	}
	fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()
	if lastSeq > 0 {
		replay, err := s.api.events.Replay(ctx, userID, lastSeq, eventTypes, 1000)
		if err != nil {
			return &apiError{err: err}
		}
		for _, event := range replay {
			if err := writeSSEEvent(w, event); err != nil {
				continue
			}
			lastSeq = event.Seq
		}
		flusher.Flush()
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-eventChan:
			if !ok {
				return nil
			}
			if event.Seq <= lastSeq {
				continue
			}
			if err := writeSSEEvent(w, event); err != nil {
				continue
			}
			lastSeq = event.Seq
			flusher.Flush()
		case <-ticker.C:
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

type responseUnwrapper interface {
	Unwrap() http.ResponseWriter
}

func responseFlusher(w http.ResponseWriter) (http.Flusher, bool) {
	for w != nil {
		if flusher, ok := w.(http.Flusher); ok {
			return flusher, true
		}
		unwrapper, ok := w.(responseUnwrapper)
		if !ok {
			return nil, false
		}
		w = unwrapper.Unwrap()
	}
	return nil, false
}

func writeSSEEvent(w io.Writer, event dto.Event) error {
	data := mapper.ToEventOutFromDTO(event)
	jsonData, err := data.MarshalJSON()
	if err != nil {
		return err
	}
	if event.Seq > 0 {
		fmt.Fprintf(w, "id: %d\n", event.Seq)
	}
	if event.Type != "" {
		fmt.Fprintf(w, "event: %s\n", event.Type)
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", jsonData)
	return err
}

func (s *rawService) FilesStream(ctx context.Context, params api.FilesStreamParams, w http.ResponseWriter) error {
	user := auth.JWTUser(ctx)
	session := &jetmodel.Sessions{UserID: auth.User(ctx), TgSession: user.TgSession}
	download := false
	if v, ok := params.Download.Get(); ok && v == api.FilesStreamDownload1 {
		download = true
	}
	return s.streamFile(ctx, w, uuid.UUID(params.ID), session, params.Range.Or(""), download)
}

func (s *rawService) SharesStream(ctx context.Context, params api.SharesStreamParams, w http.ResponseWriter) error {
	share, err := s.api.validFileShare(ctx, uuid.UUID(params.ID), params.ShareToken.Or(""))
	if err != nil {
		return err
	}
	session := &jetmodel.Sessions{UserID: share.UserID}
	download := false
	if v, ok := params.Download.Get(); ok && v == api.SharesStreamDownload1 {
		download = true
	}
	return s.streamFile(ctx, w, uuid.UUID(params.FileId), session, "", download)
}

func (s *rawService) streamFile(ctx context.Context, w http.ResponseWriter, fileID uuid.UUID, session *jetmodel.Sessions, rawRange string, download bool) error {
	logger := logging.Component("FILE").With(zap.String("file_id", fileID.String()), zap.Int64("user_id", session.UserID))
	file, err := cache.Fetch(ctx, s.api.cache, cache.KeyFile(fileID.String()), 0, func() (*jetmodel.Files, error) {
		return s.api.repo.Files.GetByID(ctx, fileID)
	})
	if err != nil {
		if errors.Is(err, repositories.ErrNotFound) {
			return &apiError{err: fileNotFound(fileID, err)}
		}
		return &apiError{err: err, code: http.StatusBadRequest}
	}
	// Do not write headers yet. First check whether varc already has the
	// requested byte range. If it does, we can serve directly from disk and
	// skip BotTokens/AuthClient/BotClient entirely.
	w.Header().Set("Accept-Ranges", "bytes")
	contentType := defaultContentType
	if file.MimeType != "" {
		contentType = file.MimeType
	}
	if file.Size == nil || *file.Size == 0 {
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Content-Length", "0")
		w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": file.Name}))
		w.WriteHeader(http.StatusOK)
		return nil
	}
	var start, end int64
	status := http.StatusOK
	if rawRange == "" {
		start = 0
		end = *file.Size - 1
	} else {
		ranges, err := http_range.Parse(rawRange, *file.Size)
		if err == http_range.ErrNoOverlap {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", *file.Size))
			return &apiError{err: http_range.ErrNoOverlap, code: http.StatusRequestedRangeNotSatisfiable}
		}
		if err != nil {
			return &apiError{err: err, code: http.StatusBadRequest}
		}
		if len(ranges) > 1 {
			return &apiError{err: fmt.Errorf("multiple ranges are not supported"), code: http.StatusRequestedRangeNotSatisfiable}
		}
		start = ranges[0].Start
		end = ranges[0].End
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, *file.Size))
		status = http.StatusPartialContent
	}
	contentLength := end - start + 1
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.FormatInt(contentLength, 10))
	w.Header().Set("ETag", fmt.Sprintf("\"%s\"", md5.FromString(fileID.String()+strconv.FormatInt(*file.Size, 10))))
	w.Header().Set("Last-Modified", file.UpdatedAt.UTC().Format(http.TimeFormat))
	disposition := "inline"
	if download {
		disposition = "attachment"
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": file.Name}))
	fingerprint := file.ID.String() + strconv.FormatInt(*file.Size, 10)

	// Fast path: serve from varc without creating any Telegram client.
	// RangeCached uses only local metadata + local data file checks.
	if s.api.varcCache != nil {
		cached, cacheErr := s.api.varcCache.RangeCached(
			file.ID.String(),
			start,
			end+1, // RangeCached end is exclusive.
			varc.WithFingerprint(fingerprint),
			varc.WithStrictFingerprint(),
		)
		if cacheErr != nil && !errors.Is(cacheErr, varc.ErrCacheMiss) {
			logger.Warn("stream.varc_range_check_failed", zap.Error(cacheErr))
		}
		if cached {
			vr, err := s.api.varcCache.Open(ctx, file.ID.String(), *file.Size, nil,
				varc.WithFingerprint(fingerprint),
				varc.WithStrictFingerprint(),
				varc.WithCacheOnly(),
			)
			if err != nil {
				logger.Warn("stream.varc_cache_only_open_failed", zap.Error(err))
			} else {
				defer vr.Close()
				w.WriteHeader(status)
				_, err = io.Copy(w, io.NewSectionReader(vr, start, contentLength))
				return err
			}
		}
	}

	// Slow path: only initialize Telegram when varc is disabled or the
	// requested range is missing locally.
	w.WriteHeader(status)
	tokens, err := s.api.channelManager.BotTokens(ctx, session.UserID)
	if err != nil {
		logger.Error("stream.bots_fetch_failed", zap.Error(err))
		return &apiError{err: fmt.Errorf("failed to get bots")}
	}
	if limit := s.api.cnf.TG.Stream.BotsLimit; limit > 0 && len(tokens) > limit {
		tokens = tokens[:limit]
	}
	var (
		client TelegramClient
		token  string
	)
	if len(tokens) == 0 {
		client, err = s.api.telegram.AuthClient(ctx, session.TgSession, 5)
		if err != nil {
			logger.Error("stream.auth_client_failed", zap.Error(err))
			return err
		}
	} else {
		token, _, err = s.api.telegram.SelectBotToken(ctx, TelegramOpStream, session.UserID, tokens)
		if err != nil {
			logger.Error("stream.bot_selection_failed", zap.Error(err))
			return err
		}
		client, err = s.api.telegram.BotClient(ctx, token, 5)
		if err != nil {
			logger.Error("stream.bot_client_failed", zap.Error(err))
			return err
		}
	}
	botID := strconv.FormatInt(session.UserID, 10)
	if token != "" {
		parts := strings.Split(token, ":")
		if len(parts) > 0 {
			botID = parts[0]
		}
	}
	handleStream := func() error {
		if file.ChannelID == nil {
			return fmt.Errorf("missing channel id")
		}
		parts, err := s.fetchParts(ctx, client, file.ID.String(), *file.ChannelID, mapper.ToAPIParts(file.Parts), file.Encrypted)
		if err != nil {
			return err
		}
		fileRef := &reader.FileRef{ID: file.ID.String(), ChannelID: *file.ChannelID, Encrypted: file.Encrypted}

		// Byte-addressable reader for the full file — used directly or wrapped in varc.
		fileReader, err := reader.NewReader(ctx, client.API(), s.api.cache, fileRef, parts, &s.api.cnf.TG, botID)
		if err != nil {
			return err
		}
		defer fileReader.Close()

		if s.api.varcCache != nil {
			vr, err := s.api.varcCache.Open(ctx, file.ID.String(), *file.Size, fileReader,
				varc.WithFingerprint(fingerprint),
				varc.WithModTime(file.UpdatedAt),
				varc.WithAttr("mime_type", contentType),
				varc.WithAttr("file_name", file.Name),
			)
			if err != nil {
				return fmt.Errorf("varc open: %w", err)
			}
			defer vr.Close()

			_, err = io.Copy(w, io.NewSectionReader(vr, start, contentLength))
			return err
		}

		// No disk cache — serve the requested HTTP range directly.
		_, err = io.Copy(w, io.NewSectionReader(fileReader, start, contentLength))
		return err
	}
	return s.api.telegram.RunWithAuth(ctx, client, token, func(ctx context.Context) error { return handleStream() })
}

func (s *rawService) fetchParts(ctx context.Context, client TelegramClient, fileID string, channelID int64, fileParts []api.Part, encrypted bool) ([]types.Part, error) {
	return cache.Fetch(ctx, s.api.cache, cache.KeyFileMessages(fileID), 60*time.Minute, func() ([]types.Part, error) {
		parts, err := s.api.telegram.GetParts(ctx, client, channelID, fileParts, encrypted)
		if err != nil {
			return nil, err
		}

		if len(parts) != len(fileParts) {
			logger := logging.Component("FILE")
			logger.Error("parts.mismatch",
				zap.String("file_id", fileID),
				zap.Int("expected", len(fileParts)),
				zap.Int("actual", len(parts)))
			return nil, fmt.Errorf("file parts mismatch")
		}
		return parts, nil
	})
}
