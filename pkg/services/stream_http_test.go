package services

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gotd/td/tgerr"
	"github.com/tgdrive/teldrive/internal/api"
	"github.com/tgdrive/teldrive/internal/cache"
	"github.com/tgdrive/teldrive/internal/config"
	"github.com/tgdrive/teldrive/pkg/models"
	"gorm.io/datatypes"
)

func TestFilesStreamDescriptorAndHeadResponsesBeforeTelegram(t *testing.T) {
	size, channel := int64(10), int64(1)
	parts := datatypes.JSONSlice[api.Part]{{ID: 1}}
	for _, tt := range []struct {
		name, method string
		file         *models.File
		status       int
		length       string
	}{
		{"malformed", "GET", &models.File{Size: &size}, 422, ""},
		{"valid head without timestamp", "HEAD", &models.File{Name: "file.txt", Size: &size, Parts: &parts, ChannelId: &channel}, 200, "10"},
		{"empty", "GET", &models.File{Name: "empty", Size: new(int64)}, 200, "0"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			c := cache.NewMemoryCache(1024 * 1024)
			if err := c.Set(ctx, "sessions:stream-test", models.Session{UserId: 1}, 0); err != nil {
				t.Fatal(err)
			}
			if err := c.Set(ctx, cache.Key("files", "file"), tt.file, 0); err != nil {
				t.Fatal(err)
			}
			e := NewExtendedService(NewApiService(nil, &config.ServerCmdConfig{}, c, nil, nil, nil))
			r := httptest.NewRequest(tt.method, "/?hash=stream-test", nil)
			w := httptest.NewRecorder()
			e.FilesStream(w, r, "file", 0)
			if w.Code != tt.status || w.Header().Get("Content-Length") != tt.length {
				t.Fatalf("%d %v", w.Code, w.Header())
			}
			if tt.method == "HEAD" && w.Body.Len() != 0 {
				t.Fatal("HEAD body")
			}
		})
	}
}

func TestExpiredTelegramCredentialsFailPermanentlyWithoutRetryHint(t *testing.T) {
	w := httptest.NewRecorder()
	writeStreamError(w, &tgerr.Error{Code: 400, Type: "ACCESS_TOKEN_EXPIRED"})
	if w.Code != 403 || w.Header().Get("Retry-After") != "" {
		t.Fatalf("%d %v", w.Code, w.Header())
	}
}
