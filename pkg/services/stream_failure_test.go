package services

import (
	"context"
	"testing"

	"github.com/tgdrive/teldrive/internal/api"
	"github.com/tgdrive/teldrive/pkg/models"
	"gorm.io/datatypes"
)

func TestGetPartsRejectsMalformedDescriptorBeforeDependencies(t *testing.T) {
	size, channel := int64(10), int64(123)
	parts := datatypes.JSONSlice[api.Part]{{ID: 1}}
	empty := datatypes.JSONSlice[api.Part]{}
	bad := datatypes.JSONSlice[api.Part]{{ID: 0}}
	for name, file := range map[string]*models.File{
		"nil file":        nil,
		"nil parts":       {Size: &size, ChannelId: &channel},
		"nil channel":     {Size: &size, Parts: &parts},
		"empty parts":     {Size: &size, ChannelId: &channel, Parts: &empty},
		"invalid message": {Size: &size, ChannelId: &channel, Parts: &bad},
		"nil size":        {ChannelId: &channel, Parts: &parts},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if p := recover(); p != nil {
					t.Fatalf("malformed descriptor panicked before dependency access: %v", p)
				}
			}()
			if _, err := getParts(context.Background(), nil, nil, file); err == nil {
				t.Fatal("expected explicit descriptor error")
			}
		})
	}
}
