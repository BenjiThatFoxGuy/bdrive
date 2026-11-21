package services

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
	"github.com/tgdrive/teldrive/internal/api"
	"github.com/tgdrive/teldrive/internal/cache"
	"github.com/tgdrive/teldrive/internal/crypt"
	"github.com/tgdrive/teldrive/internal/logging"
	"github.com/tgdrive/teldrive/internal/tgc"
	"github.com/tgdrive/teldrive/internal/utils"
	"github.com/tgdrive/teldrive/pkg/models"
	"github.com/tgdrive/teldrive/pkg/types"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func getParts(ctx context.Context, client *telegram.Client, c cache.Cacher, file *models.File) ([]types.Part, error) {
	if err := file.ValidateStream(); err != nil {
		return nil, err
	}
	if *file.Size == 0 {
		return []types.Part{}, nil
	}
	return cache.Fetch(ctx, c, cache.KeyFileMessages(file.ID), 60*time.Minute, func() ([]types.Part, error) {
		messages, err := tgc.GetMessages(ctx, client.API(), utils.Map(*file.Parts, func(part api.Part) int {
			return part.ID
		}), *file.ChannelId)

		if err != nil {
			return nil, err
		}
		parts := []types.Part{}
		documents := make(map[int]*tg.Document, len(messages))
		for _, message := range messages {
			item, ok := message.(*tg.Message)
			if !ok || item == nil {
				continue
			}
			media, ok := item.Media.(*tg.MessageMediaDocument)
			if !ok || media == nil {
				continue
			}
			document, ok := media.Document.(*tg.Document)
			if !ok || document == nil || document.Size <= 0 {
				continue
			}
			documents[item.ID] = document
		}
		for _, descriptor := range *file.Parts {
			document, ok := documents[descriptor.ID]
			if !ok {
				continue
			}
			part := types.Part{ID: int64(descriptor.ID), Size: document.Size, Salt: descriptor.Salt.Value}
			if file.IsEncrypted() {
				var err error
				part.DecryptedSize, err = crypt.DecryptedSize(document.Size)
				if err != nil || part.DecryptedSize <= 0 {
					return nil, models.ErrInvalidFile
				}
			}
			parts = append(parts, part)
		}
		if len(parts) != len(*file.Parts) {
			logger := logging.Component("FILE")
			logger.Error("parts.mismatch",
				zap.String("file_id", file.ID),
				zap.String("file_name", file.Name),
				zap.Int("expected", len(*file.Parts)),
				zap.Int("actual", len(parts)))
			return nil, errors.Join(models.ErrInvalidFile, errors.New("file parts mismatch"))
		}
		return parts, nil
	})
}

func resolvePathID(db *gorm.DB, path string, userId int64) (*string, error) {
	if !strings.HasPrefix(path, "/root") {
		path = "/root/" + strings.Trim(path, "/")
	}
	var id string
	query := `
	WITH RECURSIVE path_parts AS (
		SELECT ordinality as depth, part as name
		FROM unnest(string_to_array(trim(both '/' from ?), '/')) WITH ORDINALITY as part
	),
	max_depth AS (
		SELECT max(depth) as val FROM path_parts
	),
	hierarchy AS (
		SELECT f.id, 1 as depth
		FROM teldrive.files f
		JOIN path_parts p ON p.depth = 1 AND f.name = p.name
		WHERE f.user_id = ? AND f.parent_id IS NULL AND f.status = 'active'
		UNION ALL
		SELECT child.id, h.depth + 1
		FROM teldrive.files child
		JOIN hierarchy h ON child.parent_id = h.id
		JOIN path_parts p ON p.depth = h.depth + 1 AND child.name = p.name
		JOIN max_depth md ON h.depth < md.val
		WHERE child.status = 'active'
	)
	SELECT id FROM hierarchy WHERE depth = (SELECT val FROM max_depth) LIMIT 1;
	`
	if err := db.Raw(query, path, userId).Scan(&id).Error; err != nil {
		return nil, err
	}
	if id == "" {
		return nil, gorm.ErrRecordNotFound
	}
	return &id, nil
}
