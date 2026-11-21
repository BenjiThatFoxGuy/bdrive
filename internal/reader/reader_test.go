package reader

import (
	"context"
	"testing"
	"time"

	"github.com/tgdrive/teldrive/internal/config"
	"github.com/tgdrive/teldrive/pkg/models"
	"github.com/tgdrive/teldrive/pkg/types"
)

func TestNewReaderRejectsInvalidLayoutBeforeNetwork(t *testing.T) {
	size, channel := int64(10), int64(1)
	file := &models.File{Size: &size, ChannelId: &channel}
	cfg := &config.TGConfig{}
	cfg.Stream.Concurrency = 1
	cfg.Stream.Buffers = 1
	cfg.Stream.ChunkTimeout = time.Second
	for name, parts := range map[string][]types.Part{
		"empty": nil, "zero size": {{ID: 1}}, "short": {{ID: 1, Size: 5}},
		"unequal middle": {{ID: 1, Size: 4}, {ID: 2, Size: 3}, {ID: 3, Size: 3}},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if p := recover(); p != nil {
					t.Fatalf("reader panicked: %v", p)
				}
			}()
			if _, err := NewReader(context.Background(), nil, nil, file, parts, 0, 9, cfg, ""); err == nil {
				t.Fatal("expected layout error")
			}
		})
	}
}
