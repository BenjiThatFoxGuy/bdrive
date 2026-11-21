package reader

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/tgdrive/teldrive/internal/config"
)

type failingChunks struct{ mode string }

var testChunkFailure = errors.New("Telegram unavailable")

func (s failingChunks) ChunkSize(int64, int64) int64 { return 4 }
func (s failingChunks) Chunk(context.Context, int64, int64) ([]byte, error) {
	switch s.mode {
	case "panic":
		panic("request-owned chunk panic")
	case "short":
		return []byte("a"), nil
	case "error":
		return nil, testChunkFailure
	}
	return []byte("data"), nil
}

func TestChunkWorkerFailuresStayInRequest(t *testing.T) {
	for _, mode := range []string{"error", "short", "panic", "success"} {
		t.Run(mode, func(t *testing.T) {
			cfg := &config.TGConfig{}
			cfg.Stream.Concurrency = 1
			cfg.Stream.Buffers = 1
			cfg.Stream.ChunkTimeout = time.Second
			r, err := newTGMultiReader(context.Background(), 0, 3, cfg, failingChunks{mode})
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			data, err := io.ReadAll(r)
			if mode == "success" {
				if err != nil || string(data) != "data" {
					t.Fatalf("%q %v", data, err)
				}
				return
			}
			if err == nil {
				t.Fatal("missing chunk failure")
			}
			if mode == "error" && !errors.Is(err, testChunkFailure) {
				t.Fatalf("lost dependency error: %v", err)
			}
		})
	}
}
