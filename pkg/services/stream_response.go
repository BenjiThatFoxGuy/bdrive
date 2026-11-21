package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/gotd/td/tgerr"
	"github.com/tgdrive/teldrive/internal/admission"
	"github.com/tgdrive/teldrive/internal/auth"
	"github.com/tgdrive/teldrive/pkg/models"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

var streamSetupAdmission = admission.New(1024, 2, time.Now)
var errStreamPanic = errors.New("stream request failed")

// writeStream recovers only the request-owned callback, on the goroutine where
// gotd invokes it. Authentication and the first bounded read precede success.
func writeStream(ctx context.Context, w http.ResponseWriter, status int, length int64,
	run func(context.Context, func(context.Context) error) error,
	open func(context.Context) (io.ReadCloser, error), prepare func(), logger *zap.Logger) (committed bool, err error) {
	err = run(ctx, func(ctx context.Context) (err error) {
		defer func() {
			if p := recover(); p != nil {
				logger.Error("stream.callback_panic", zap.ByteString("stack", debug.Stack()))
				err = errStreamPanic
			}
		}()
		lr, err := open(ctx)
		if err != nil {
			return err
		}
		if lr == nil {
			return errors.New("stream reader unavailable")
		}
		defer lr.Close()
		first := make([]byte, min(length, 32*1024))
		n, readErr := lr.Read(first)
		if n == 0 {
			if readErr != nil {
				return readErr
			}
			return io.ErrNoProgress
		}
		if readErr != nil && readErr != io.EOF {
			return readErr
		}
		prepare()
		w.WriteHeader(status)
		committed = true
		if _, err = w.Write(first[:n]); err != nil {
			return err
		}
		if int64(n) < length {
			_, err = io.CopyN(w, lr, length-int64(n))
		}
		return err
	})
	return committed, err
}

func streamStatus(err error) int {
	switch {
	case errors.Is(err, models.ErrInvalidFile):
		return http.StatusUnprocessableEntity
	case errors.Is(err, gorm.ErrRecordNotFound):
		return http.StatusNotFound
	}
	// Telegram rejects expired/revoked credentials permanently. Never retry these
	// as transient network outages or expose raw Telegram/session errors to clients.
	if rpc, ok := tgerr.As(err); ok {
		if rpc.Code == 401 || rpc.Code == 403 || rpc.Type == "ACCESS_TOKEN_EXPIRED" || rpc.Type == "ACCESS_TOKEN_INVALID" {
			return http.StatusForbidden
		}
	}
	return http.StatusServiceUnavailable
}

func writeStreamError(w http.ResponseWriter, err error) {
	code := streamStatus(err)
	for _, key := range []string{"Content-Length", "Content-Range", "ETag", "Last-Modified", "Content-Disposition"} {
		w.Header().Del(key)
	}
	if code == 503 {
		w.Header().Set("Retry-After", "30")
	}
	message := http.StatusText(code)
	if code == 422 {
		message = "invalid file descriptor or missing file parts"
	}
	if code == 403 {
		message = "Telegram credentials rejected; sign in again or replace the expired bot token"
	}
	http.Error(w, message, code)
}

func writeStreamAuthError(w http.ResponseWriter, err error) {
	if errors.Is(err, auth.ErrUnavailable) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, admission.ErrBusy) {
		writeStreamError(w, fmt.Errorf("%w: %w", auth.ErrUnavailable, err))
		return
	}
	http.Error(w, "invalid credentials", http.StatusUnauthorized)
}
