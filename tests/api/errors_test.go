package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/tgdrive/teldrive/internal/api"
)

// errorBody mirrors the JSON shape of api.Error for raw HTTP assertions.
type errorBody struct {
	Code      int    `json:"code"`
	Error     string `json:"error,omitempty"`
	Message   string `json:"message"`
	RequestID string `json:"requestId,omitempty"`
}

func decodeError(t *testing.T, body []byte) errorBody {
	t.Helper()
	var e errorBody
	if err := json.Unmarshal(body, &e); err != nil {
		t.Fatalf("decode error body: %v body=%s", err, string(body))
	}
	return e
}

func TestErrorContract_UnauthenticatedReturns401(t *testing.T) {
	s := newHarness(t)

	// No auth at all — any protected endpoint returns 401.
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, s.server.URL+"/files", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := s.httpCli.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}

	// X-Request-ID must be set.
	if resp.Header.Get("X-Request-ID") == "" {
		t.Fatal("expected X-Request-ID header")
	}
}

func TestErrorContract_BadUUIDReturns400(t *testing.T) {
	s := newHarness(t)
	token := loginAndGetToken(t, s, 9001, "error-user-9001")
	client := s.newClientWithToken(token)

	ctx := context.Background()
	_, err := client.FilesGetById(ctx, api.FilesGetByIdParams{ID: api.UUID(uuid.MustParse("00000000-0000-0000-0000-000000000000"))})
	if statusCode(err) != 404 {
		t.Fatalf("expected 404, got %d err=%v", statusCode(err), err)
	}

	// Also test via raw HTTP for full body inspection.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.server.URL+"/files/bad-uuid", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Cookie", "access_token="+token)

	resp, err := s.httpCli.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	body := make([]byte, 4096)
	n, _ := resp.Body.Read(body)
	e := decodeError(t, body[:n])
	if e.Code != 0 && e.Code != 400 {
		t.Fatalf("expected body.code=400, got %d", e.Code)
	}
	if e.Code != 0 && e.Error == "" {
		t.Fatal("expected non-empty body.error")
	}
	if e.Code != 0 && e.Message == "" {
		t.Fatal("expected non-empty body.message")
	}
	if e.Code != 0 && e.RequestID == "" {
		t.Fatal("expected non-empty body.requestId")
	}
	if resp.Header.Get("X-Request-ID") == "" {
		t.Fatal("expected X-Request-ID header in response")
	}
}

func TestErrorContract_FileNotFoundReturns404(t *testing.T) {
	s := newHarness(t)
	_, client, _ := loginWithClient(t, s, 9002, "error-user-9002")
	ctx := context.Background()

	nonexistentID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	_, err := client.FilesGetById(ctx, api.FilesGetByIdParams{ID: api.UUID(nonexistentID)})
	if statusCode(err) != 404 {
		t.Fatalf("expected 404, got %d err=%v", statusCode(err), err)
	}
}

func TestErrorContract_MissingFieldReturns409(t *testing.T) {
	s := newHarness(t)
	_, client, _ := loginWithClient(t, s, 9003, "error-user-9003")
	ctx := context.Background()

	// Creating a folder without path or parent should return 409.
	_, err := client.FilesCreate(ctx, &api.File{Name: "nopath", Type: api.FileTypeFolder})
	if statusCode(err) != 409 {
		t.Fatalf("expected 409, got %d err=%v", statusCode(err), err)
	}
}

func TestErrorContract_RequestIDEchoed(t *testing.T) {
	s := newHarness(t)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, s.server.URL+"/files", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("X-Request-ID", "my-custom-id-123")

	resp, err := s.httpCli.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	got := resp.Header.Get("X-Request-ID")
	if got != "my-custom-id-123" {
		t.Fatalf("expected X-Request-ID=my-custom-id-123, got %q", got)
	}
}

func TestErrorContract_RequestIDGeneratedWhenMissing(t *testing.T) {
	s := newHarness(t)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, s.server.URL+"/files", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	// No X-Request-ID header.

	resp, err := s.httpCli.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	got := resp.Header.Get("X-Request-ID")
	if got == "" {
		t.Fatal("expected X-Request-ID to be generated")
	}
	// Verify it's a UUID format.
	if _, err := uuid.Parse(got); err != nil {
		t.Fatalf("expected generated X-Request-ID to be a UUID, got %q: %v", got, err)
	}
}

func TestErrorContract_ForbiddenSharePassword(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	_, client, _ := loginWithClient(t, s, 9004, "error-user-9004")

	if err := client.UsersUpdateChannel(ctx, &api.ChannelUpdate{ChannelId: api.NewOptInt64(910081), ChannelName: api.NewOptString("error-default")}); err != nil {
		t.Fatalf("UsersUpdateChannel failed: %v", err)
	}

	folder, err := client.FilesCreate(ctx, &api.File{Name: "pw-folder", Type: api.FileTypeFolder, Path: api.NewOptString("/")})
	if err != nil {
		t.Fatalf("FilesCreate folder failed: %v", err)
	}

	// Create a password-protected share.
	if err := client.FilesCreateShare(ctx, &api.FileShareCreate{Password: api.NewOptString("secret123")}, api.FilesCreateShareParams{ID: folder.ID.Value}); err != nil {
		t.Fatalf("FilesCreateShare protected failed: %v", err)
	}

	shares, err := client.FilesListShares(ctx, api.FilesListSharesParams{ID: folder.ID.Value})
	if err != nil || len(shares) == 0 {
		t.Fatalf("FilesListShares failed: %v len=%d", err, len(shares))
	}

	var protectedID api.UUID
	for _, sh := range shares {
		if sh.Protected {
			protectedID = sh.ID
			break
		}
	}
	if uuid.UUID(protectedID) == uuid.Nil {
		t.Fatal("expected protected share")
	}

	// Attempt to list without unlock — should be 401.
	_, err = client.SharesListFiles(ctx, api.SharesListFilesParams{ID: protectedID, Limit: api.NewOptInt(20)})
	if statusCode(err) != 401 {
		t.Fatalf("expected 401 without unlock, got %d err=%v", statusCode(err), err)
	}

	// Wrong password returns 403.
	_, err = client.SharesUnlock(ctx, &api.ShareUnlock{Password: "wrong"}, api.SharesUnlockParams{ID: protectedID})
	if statusCode(err) != 403 {
		t.Fatalf("expected 403 for wrong password, got %d err=%v", statusCode(err), err)
	}
}

func TestErrorContract_InvalidRange(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	token := loginAndGetToken(t, s, 9005, "error-user-9005")
	client := s.newClientWithToken(token)

	if err := client.UsersUpdateChannel(ctx, &api.ChannelUpdate{ChannelId: api.NewOptInt64(910082), ChannelName: api.NewOptString("stream-default")}); err != nil {
		t.Fatalf("UsersUpdateChannel failed: %v", err)
	}

	file, err := client.FilesCreate(ctx, &api.File{
		Name:      "stream-range.txt",
		Type:      api.FileTypeFile,
		Path:      api.NewOptString("/"),
		MimeType:  api.NewOptString("text/plain"),
		ChannelId: api.NewOptInt64(910082),
		Size:      api.NewOptInt64(100),
	})
	if err != nil {
		t.Fatalf("FilesCreate file failed: %v", err)
	}

	// GET with range way beyond file size should produce 416 before streaming.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.server.URL+"/files/"+uuid.UUID(file.ID.Value).String()+"/content", nil)
	if err != nil {
		t.Fatalf("new HEAD request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Cookie", "access_token="+token)
	req.Header.Set("Range", "bytes=1000-2000")

	resp, err := s.httpCli.Do(req)
	if err != nil {
		t.Fatalf("HEAD request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("expected 416 for invalid range, got %d", resp.StatusCode)
	}
}
