package services

import (
	"bytes"
	"context"
	"fmt"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"

	"github.com/tgdrive/teldrive/internal/logging"
	"github.com/tgdrive/teldrive/internal/tgc"
	"go.uber.org/zap"
)

// uploadWithChannelFallback uploads a blob to the preferred channel, falling
// back to the user's default channel on ANY failure. It returns the channel that
// actually accepted the upload and the resulting message ID.
func (a *apiService) uploadWithChannelFallback(
	ctx context.Context,
	client *telegram.Client,
	userId int64,
	preferredChannel int64,
	data []byte,
	size int64,
	filename string,
	threads int,
) (int64, int, error) {
	msgID, err := uploadBlob(ctx, client, preferredChannel, data, size, filename, threads)
	if err == nil {
		return preferredChannel, msgID, nil
	}

	logging.Component("UPLOAD").Warn("upload.failed_trying_default",
		zap.Error(err), zap.Int64("channel_id", preferredChannel))

	defaultCh, chErr := a.channelManager.GetChannel(ctx, userId)
	if chErr != nil || defaultCh == preferredChannel {
		return 0, 0, err
	}

	msgID, err = uploadBlob(ctx, client, defaultCh, data, size, filename, threads)
	if err != nil {
		return 0, 0, err
	}
	return defaultCh, msgID, nil
}

// uploadBlob uploads one document to a Telegram channel and returns its message ID.
func uploadBlob(ctx context.Context, client *telegram.Client, channelID int64, data []byte, size int64, filename string, threads int) (int, error) {
	channel, err := tgc.GetChannelById(ctx, client.API(), channelID)
	if err != nil {
		return 0, err
	}
	u := uploader.NewUploader(client.API()).WithThreads(threads).WithPartSize(512 * 1024)
	upload, err := u.Upload(ctx, uploader.NewUpload(filename, bytes.NewReader(data), size))
	if err != nil {
		return 0, err
	}
	doc := message.UploadedDocument(upload).Filename(filename).ForceFile(true)
	sender := message.NewSender(client.API())
	target := sender.To(&tg.InputPeerChannel{ChannelID: channel.ChannelID, AccessHash: channel.AccessHash})
	res, err := target.Media(ctx, doc)
	if err != nil {
		return 0, err
	}
	updates, ok := res.(*tg.Updates)
	if !ok {
		return 0, fmt.Errorf("upload: unexpected response type %T", res)
	}
	for _, update := range updates.Updates {
		if cm, ok := update.(*tg.UpdateNewChannelMessage); ok {
			if msg, ok := cm.Message.AsNotEmpty(); ok {
				if m, ok := msg.(*tg.Message); ok && m.ID != 0 {
					return m.ID, nil
				}
			}
		}
	}
	return 0, fmt.Errorf("upload failed: no message in response")
}
