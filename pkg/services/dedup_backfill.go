package services

import (
	"context"
	"errors"
	"io"
	"strconv"
	"strings"

	"github.com/gotd/td/telegram"
	"github.com/tgdrive/teldrive/internal/cache"
	"github.com/tgdrive/teldrive/internal/config"
	"github.com/tgdrive/teldrive/internal/hash"
	"github.com/tgdrive/teldrive/internal/reader"
	"github.com/tgdrive/teldrive/internal/tgc"
	"github.com/tgdrive/teldrive/pkg/models"
	"gorm.io/gorm"
)

// ComputeFileContentHash computes the BLAKE3 tree hash of a file's content by
// re-reading it back from Telegram. Used to backfill a hash for files that
// were created without one (e.g. direct-parts uploads, or files that predate
// the dedup feature), so they can participate in deduplication like any file
// hashed during upload.
func ComputeFileContentHash(ctx context.Context, client *telegram.Client, cacher cache.Cacher,
	cnf *config.TGConfig, botID string, file *models.File) (string, error) {

	if file.Size == nil || *file.Size == 0 {
		return hash.SumToHex(hash.ComputeTreeHash(nil)), nil
	}

	parts, err := getParts(ctx, client, cacher, file)
	if err != nil {
		return "", err
	}

	lr, err := reader.NewReader(ctx, client.API(), cacher, file, parts, 0, *file.Size-1, cnf, botID)
	if err != nil {
		return "", err
	}
	defer lr.Close()

	blockHasher := hash.NewBlockHasher()
	if _, err := io.Copy(blockHasher, lr); err != nil {
		return "", err
	}

	return hash.SumToHex(hash.ComputeTreeHash(blockHasher.Sum())), nil
}

// ResolveUserClient resolves a Telegram client for userId, preferring a
// configured bot (consistent with how uploads/streaming pick a bot) and
// falling back to fallbackSession (a Telethon session string) when the user
// has no bots configured. Returns the client, the bot token used (empty if
// the session fallback was used), and the botID to use for cache keys.
func ResolveUserClient(ctx context.Context, db *gorm.DB, cacher cache.Cacher, cnf *config.TGConfig,
	channelManager *tgc.ChannelManager, botSelector tgc.BotSelector, userId int64,
	fallbackSession string) (client *telegram.Client, token string, botID string, err error) {

	tokens, err := channelManager.BotTokens(ctx, userId)
	if err != nil {
		return nil, "", "", err
	}

	if len(tokens) > 0 {
		token, _, err = botSelector.Next(ctx, tgc.BotOpStream, userId, tokens)
		if err != nil {
			return nil, "", "", err
		}
		client, err = tgc.BotClient(ctx, db, cacher, cnf, token)
		if err != nil {
			return nil, "", "", err
		}
		botID = token
		if parts := strings.Split(token, ":"); len(parts) > 0 {
			botID = parts[0]
		}
		return client, token, botID, nil
	}

	if fallbackSession == "" {
		return nil, "", "", errors.New("no bots configured and no telegram session available for user")
	}

	client, err = tgc.AuthClient(ctx, cnf, fallbackSession)
	if err != nil {
		return nil, "", "", err
	}
	return client, "", strconv.FormatInt(userId, 10), nil
}
