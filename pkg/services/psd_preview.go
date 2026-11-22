package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/tgdrive/teldrive/internal/api"
	"github.com/tgdrive/teldrive/internal/auth"
	"github.com/tgdrive/teldrive/internal/psdconv"
	"github.com/tgdrive/teldrive/internal/tgc"
	"github.com/tgdrive/teldrive/pkg/models"
	"gorm.io/gorm"
)

const maxPSDPreviewSource = 512 << 20

// One source and one worker per instance. The source is an unlinked file, so
// crashes cannot leave decrypted PSDs behind. The timer closes the idle fd.
var psdPreviewState struct {
	sync.Mutex
	file     *os.File
	key      string
	timer    *time.Timer
	revision uint64
}
var psdPreviewSlot = make(chan struct{}, 1)

func (a *apiService) psdPreview(ctx context.Context, id, mode string, overrides map[int]bool) ([]byte, error) {
	user := auth.GetJWTUser(ctx)
	userID := auth.GetUser(ctx)
	if user == nil || userID == 0 {
		return nil, &apiError{code: http.StatusUnauthorized, err: errors.New("unauthenticated")}
	}
	if !psdconv.WorkerSupported() {
		return nil, &apiError{code: http.StatusNotImplemented, err: psdconv.ErrUnsupportedPreview}
	}
	var f models.File
	if err := a.db.WithContext(ctx).Where("id = ? AND user_id = ? AND status = ?", id, userID, "active").First(&f).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, &apiError{code: http.StatusNotFound, err: err}
		}
		return nil, err
	}
	if !psdconv.IsPSD(f.MimeType, f.Name) || f.ValidateStream() != nil || *f.Size == 0 || *f.Size > maxPSDPreviewSource {
		return nil, &apiError{code: http.StatusUnprocessableEntity, err: psdconv.ErrUnsupportedPreview}
	}
	select {
	case psdPreviewSlot <- struct{}{}:
		defer func() { <-psdPreviewSlot }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	key := fmt.Sprintf("%d:%s:%d:%v", userID, f.ID, *f.Size, f.UpdatedAt)
	psdPreviewState.Lock()
	psdPreviewState.revision++
	revision := psdPreviewState.revision
	if psdPreviewState.timer != nil {
		psdPreviewState.timer.Stop()
	}
	source := psdPreviewState.file
	if source != nil && psdPreviewState.key != key {
		_ = source.Close()
		source = nil
		psdPreviewState.file = nil
	}
	psdPreviewState.Unlock()
	if source == nil {
		var err error
		source, err = NewExtendedService(a).spoolPSDPreview(ctx, &f, &models.Session{UserId: userID, Session: user.TgSession})
		if err != nil {
			return nil, err
		}
	}
	defer func() {
		psdPreviewState.Lock()
		defer psdPreviewState.Unlock()
		psdPreviewState.key, psdPreviewState.file = key, source
		psdPreviewState.timer = time.AfterFunc(60*time.Second, func() {
			psdPreviewState.Lock()
			defer psdPreviewState.Unlock()
			if psdPreviewState.revision != revision {
				return
			}
			_ = source.Close()
			psdPreviewState.file = nil
		})
	}()
	data, err := psdconv.RunPreviewWorker(ctx, source, mode, overrides)
	if errors.Is(err, psdconv.ErrUnsupportedPreview) {
		return nil, &apiError{code: http.StatusUnprocessableEntity, err: err}
	}
	return data, err
}

func (e *extendedService) spoolPSDPreview(ctx context.Context, f *models.File, session *models.Session) (*os.File, error) {
	tmp, err := os.CreateTemp("", "bdrive-psd-preview-*")
	if err != nil {
		return nil, err
	}
	success := false
	defer func() {
		if !success {
			_ = tmp.Close()
		}
	}()
	if err = os.Remove(tmp.Name()); err != nil {
		return nil, err
	}
	copySource := func(r io.Reader) error {
		_, err := io.CopyN(tmp, r, *f.Size)
		return err
	}
	if cached, ok := e.openCachedFile(f.ID, 0, *f.Size); ok {
		err = copySource(cached)
		_ = cached.Close()
	} else {
		client, token, botID, clientErr := e.resolveStreamClient(ctx, session)
		if clientErr != nil {
			return nil, clientErr
		}
		err = tgc.RunWithAuth(ctx, client, token, func(ctx context.Context) error {
			r, src, err := e.openForThumbnail(ctx, client, f, botID)
			if err != nil {
				return err
			}
			defer src.Close()
			return copySource(r)
		})
	}
	if err != nil {
		return nil, err
	}
	success = true
	return tmp, nil
}

func (a *apiService) FilesPsdPreviewMetadata(ctx context.Context, params api.FilesPsdPreviewMetadataParams) (*api.PsdPreviewMetadata, error) {
	data, err := a.psdPreview(ctx, params.ID, "inspect", nil)
	if err != nil {
		return nil, err
	}
	var meta api.PsdPreviewMetadata
	if err = json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

func (a *apiService) FilesPsdPreviewRender(ctx context.Context, req *api.PsdPreviewRequest, params api.FilesPsdPreviewRenderParams) (api.FilesPsdPreviewRenderOK, error) {
	overrides := make(map[int]bool, len(req.Visibility))
	for _, v := range req.Visibility {
		if _, exists := overrides[int(v.ID)]; exists {
			return api.FilesPsdPreviewRenderOK{}, &apiError{code: http.StatusBadRequest, err: errors.New("duplicate PSD layer ID")}
		}
		overrides[int(v.ID)] = v.Visible
	}
	data, err := a.psdPreview(ctx, params.ID, "render", overrides)
	if err != nil {
		return api.FilesPsdPreviewRenderOK{}, err
	}
	return api.FilesPsdPreviewRenderOK{Data: bytes.NewReader(data)}, nil
}
