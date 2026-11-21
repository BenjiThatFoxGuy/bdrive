package services

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tgdrive/teldrive/internal/config"
	"go.uber.org/zap"
)

func TestStreamInvalidCookieStopsBeforeDependencies(t *testing.T) {
	e := &extendedService{api: &apiService{cnf: &config.ServerCmdConfig{}}}
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(&http.Cookie{Name: authCookieName, Value: "invalid"})
	w := httptest.NewRecorder()
	defer func() {
		if p := recover(); p != nil {
			t.Fatalf("invalid cookie panicked: %v", p)
		}
	}()
	e.FilesStream(w, r, "file", 0)
	if w.Code != 401 {
		t.Fatalf("status %d", w.Code)
	}
}

func TestStreamSetupFailuresDoNotCommitSuccess(t *testing.T) {
	for _, mode := range []string{"auth", "reader", "panic"} {
		t.Run(mode, func(t *testing.T) {
			w := httptest.NewRecorder()
			run := func(ctx context.Context, f func(context.Context) error) error {
				if mode == "auth" {
					return errors.New("dependency unavailable")
				}
				ch := make(chan error, 1)
				go func() { ch <- f(ctx) }()
				return <-ch
			}
			open := func(context.Context) (io.ReadCloser, error) {
				if mode == "panic" {
					panic("request-owned failure")
				}
				return nil, errors.New("reader unavailable")
			}
			committed, err := writeStream(context.Background(), w, 206, 4, run, open, func() { w.Header().Set("Content-Length", "4") }, zap.NewNop())
			if err == nil || committed {
				t.Fatalf("committed=%v err=%v", committed, err)
			}
			writeStreamError(w, err)
			if w.Code != 503 || w.Header().Get("Retry-After") == "" {
				t.Fatalf("response %d %v", w.Code, w.Header())
			}
		})
	}
}

func TestStreamSuccessIsUnchanged(t *testing.T) {
	w := httptest.NewRecorder()
	run := func(ctx context.Context, f func(context.Context) error) error { return f(ctx) }
	committed, err := writeStream(context.Background(), w, 206, 4, run, func(context.Context) (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("data")), nil }, func() { w.Header().Set("Content-Length", "4") }, zap.NewNop())
	if err != nil || !committed || w.Code != 206 || w.Body.String() != "data" {
		t.Fatalf("%v %v %d %q", committed, err, w.Code, w.Body.String())
	}
}

func TestStreamReadFailureDoesNotAppendErrorText(t *testing.T) {
	w := httptest.NewRecorder()
	run := func(ctx context.Context, f func(context.Context) error) error { return f(ctx) }
	committed, err := writeStream(context.Background(), w, 206, 4, run, func(context.Context) (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("a")), nil }, func() {}, zap.NewNop())
	// A first bounded read stages only one byte; a later short read must be returned,
	// never replaced with an HTTP error body after success has been committed.
	if err == nil || !committed || w.Body.String() != "a" {
		t.Fatalf("%v %v %q", committed, err, w.Body.String())
	}
}
