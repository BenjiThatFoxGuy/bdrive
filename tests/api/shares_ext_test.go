package api_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/tgdrive/teldrive/internal/api"
)

func TestSharesStream(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	_, client, _ := loginWithClient(t, s, 8404, "user8404")

	if err := client.UsersUpdateChannel(ctx, &api.ChannelUpdate{ChannelId: api.NewOptInt64(910084), ChannelName: api.NewOptString("stream-share-test")}); err != nil {
		t.Fatalf("UsersUpdateChannel failed: %v", err)
	}

	// Create a file with zero size — streamFile returns 200 immediately
	// without any Telegram interaction for zero-length files.
	file, err := client.FilesCreate(ctx, &api.File{
		Name:      "stream-share.txt",
		Type:      api.FileTypeFile,
		Path:      api.NewOptString("/"),
		MimeType:  api.NewOptString("text/plain"),
		ChannelId: api.NewOptInt64(910084),
		Size:      api.NewOptInt64(0),
	})
	if err != nil {
		t.Fatalf("FilesCreate file failed: %v", err)
	}

	// Create an unprotected share.
	if err := client.FilesCreateShare(ctx, &api.FileShareCreate{}, api.FilesCreateShareParams{ID: file.ID.Value}); err != nil {
		t.Fatalf("FilesCreateShare failed: %v", err)
	}

	shares, err := client.FilesListShares(ctx, api.FilesListSharesParams{ID: file.ID.Value})
	if err != nil || len(shares) == 0 {
		t.Fatalf("FilesListShares failed: %v len=%d", err, len(shares))
	}

	// Hit the raw SharesStream endpoint.
	shareID := uuid.UUID(shares[0].ID)
	fileID := uuid.UUID(file.ID.Value)
	reqURL := s.server.URL + "/shares/" + shareID.String() + "/files/" + fileID.String() + "/content"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	resp, err := s.httpCli.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestFilesEditShare(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	_, client, _ := loginWithClient(t, s, 8405, "user8405")

	if err := client.UsersUpdateChannel(ctx, &api.ChannelUpdate{ChannelId: api.NewOptInt64(910085), ChannelName: api.NewOptString("edit-share-test")}); err != nil {
		t.Fatalf("UsersUpdateChannel failed: %v", err)
	}

	file, err := client.FilesCreate(ctx, &api.File{
		Name:      "edit-share.txt",
		Type:      api.FileTypeFile,
		Path:      api.NewOptString("/"),
		MimeType:  api.NewOptString("text/plain"),
		ChannelId: api.NewOptInt64(910085),
		Size:      api.NewOptInt64(12),
	})
	if err != nil {
		t.Fatalf("FilesCreate file failed: %v", err)
	}

	// Create an unprotected share.
	if err := client.FilesCreateShare(ctx, &api.FileShareCreate{}, api.FilesCreateShareParams{ID: file.ID.Value}); err != nil {
		t.Fatalf("FilesCreateShare failed: %v", err)
	}

	shares, err := client.FilesListShares(ctx, api.FilesListSharesParams{ID: file.ID.Value})
	if err != nil || len(shares) == 0 {
		t.Fatalf("FilesListShares failed: %v len=%d", err, len(shares))
	}

	// Edit the share to add a password.
	if err := client.FilesEditShare(ctx, &api.FileShareCreate{Password: api.NewOptString("newpw")}, api.FilesEditShareParams{ID: file.ID.Value, ShareId: shares[0].ID}); err != nil {
		t.Fatalf("FilesEditShare failed: %v", err)
	}
}

func TestFilesDeleteShare(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	_, client, _ := loginWithClient(t, s, 8406, "user8406")

	if err := client.UsersUpdateChannel(ctx, &api.ChannelUpdate{ChannelId: api.NewOptInt64(910086), ChannelName: api.NewOptString("delete-share-test")}); err != nil {
		t.Fatalf("UsersUpdateChannel failed: %v", err)
	}

	file, err := client.FilesCreate(ctx, &api.File{
		Name:      "delete-share.txt",
		Type:      api.FileTypeFile,
		Path:      api.NewOptString("/"),
		MimeType:  api.NewOptString("text/plain"),
		ChannelId: api.NewOptInt64(910086),
		Size:      api.NewOptInt64(12),
	})
	if err != nil {
		t.Fatalf("FilesCreate file failed: %v", err)
	}

	// Create an unprotected share.
	if err := client.FilesCreateShare(ctx, &api.FileShareCreate{}, api.FilesCreateShareParams{ID: file.ID.Value}); err != nil {
		t.Fatalf("FilesCreateShare failed: %v", err)
	}

	shares, err := client.FilesListShares(ctx, api.FilesListSharesParams{ID: file.ID.Value})
	if err != nil || len(shares) == 0 {
		t.Fatalf("FilesListShares failed: %v len=%d", err, len(shares))
	}

	// Delete the share.
	if err := client.FilesDeleteShare(ctx, api.FilesDeleteShareParams{ID: file.ID.Value, ShareId: shares[0].ID}); err != nil {
		t.Fatalf("FilesDeleteShare failed: %v", err)
	}

	// Verify the share is gone — SharesGetById should return 404.
	_, err = client.SharesGetById(ctx, api.SharesGetByIdParams{ID: shares[0].ID})
	if statusCode(err) != 404 {
		t.Fatalf("expected 404 after delete, got %d err=%v", statusCode(err), err)
	}
}
