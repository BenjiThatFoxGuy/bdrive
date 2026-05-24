package api_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/tgdrive/teldrive/internal/api"
)

func TestStreamFile_MissingReturns404(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	token := loginAndGetToken(t, s, 9101, "stream-user-9101")

	nonexistentUUID := uuid.MustParse("00000000-0000-0000-0000-000000000001")

	// HEAD missing file => 404
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, s.server.URL+"/files/"+nonexistentUUID.String()+"/content", nil)
	if err != nil {
		t.Fatalf("new HEAD request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Cookie", "access_token="+token)

	resp, err := s.httpCli.Do(req)
	if err != nil {
		t.Fatalf("HEAD request failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for missing file HEAD, got %d", resp.StatusCode)
	}

	// GET missing file => 404
	req, err = http.NewRequestWithContext(ctx, http.MethodGet, s.server.URL+"/files/"+nonexistentUUID.String()+"/content", nil)
	if err != nil {
		t.Fatalf("new GET request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Cookie", "access_token="+token)

	resp, err = s.httpCli.Do(req)
	if err != nil {
		t.Fatalf("GET request failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for missing file GET, got %d", resp.StatusCode)
	}
}

func TestStreamFile_InvalidRangeReturns416(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	token := loginAndGetToken(t, s, 9102, "stream-user-9102")
	client := s.newClientWithToken(token)

	if err := client.UsersUpdateChannel(ctx, &api.ChannelUpdate{ChannelId: api.NewOptInt64(910082), ChannelName: api.NewOptString("stream-range")}); err != nil {
		t.Fatalf("UsersUpdateChannel failed: %v", err)
	}

	file, err := client.FilesCreate(ctx, &api.File{
		Name:      "range-test.txt",
		Type:      api.FileTypeFile,
		Path:      api.NewOptString("/"),
		MimeType:  api.NewOptString("text/plain"),
		ChannelId: api.NewOptInt64(910082),
		Size:      api.NewOptInt64(100),
	})
	if err != nil {
		t.Fatalf("FilesCreate file failed: %v", err)
	}

	// GET with range beyond file size => 416 before streaming begins.
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
	resp.Body.Close()
	if resp.StatusCode != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("expected 416 for invalid range, got %d", resp.StatusCode)
	}

	// Valid suffix range should be accepted.
	req, err = http.NewRequestWithContext(ctx, http.MethodGet, s.server.URL+"/files/"+uuid.UUID(file.ID.Value).String()+"/content", nil)
	if err != nil {
		t.Fatalf("new GET request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Cookie", "access_token="+token)
	req.Header.Set("Range", "bytes=-5")

	resp, err = s.httpCli.Do(req)
	if err != nil {
		t.Fatalf("GET request failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("expected 206 for valid suffix range, got %d", resp.StatusCode)
	}
}

func TestStreamFile_ValidRangeAccepted(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	token := loginAndGetToken(t, s, 9103, "stream-user-9103")
	client := s.newClientWithToken(token)

	if err := client.UsersUpdateChannel(ctx, &api.ChannelUpdate{ChannelId: api.NewOptInt64(910083), ChannelName: api.NewOptString("stream-valid")}); err != nil {
		t.Fatalf("UsersUpdateChannel failed: %v", err)
	}

	file, err := client.FilesCreate(ctx, &api.File{
		Name:      "valid-range.txt",
		Type:      api.FileTypeFile,
		Path:      api.NewOptString("/"),
		MimeType:  api.NewOptString("text/plain"),
		ChannelId: api.NewOptInt64(910083),
		Size:      api.NewOptInt64(100),
	})
	if err != nil {
		t.Fatalf("FilesCreate file failed: %v", err)
	}

	// HEAD is not generated for this route; verify the stored metadata through GET
	// range validation in TestStreamFile_InvalidRangeReturns416.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.server.URL+"/files/"+uuid.UUID(file.ID.Value).String()+"/content", nil)
	if err != nil {
		t.Fatalf("new HEAD request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Cookie", "access_token="+token)
	req.Header.Set("Range", "bytes=0-49")

	resp, err := s.httpCli.Do(req)
	if err != nil {
		t.Fatalf("HEAD request failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("expected 206 for valid range, got %d", resp.StatusCode)
	}
	if resp.Header.Get("Accept-Ranges") != "bytes" {
		t.Fatalf("expected Accept-Ranges: bytes, got %q", resp.Header.Get("Accept-Ranges"))
	}
	cr := resp.Header.Get("Content-Range")
	if cr == "" {
		t.Fatal("expected Content-Range header for 206 response")
	}
	// Content-Range should be "bytes 0-49/100"
	expectedCR := "bytes 0-49/100"
	if cr != expectedCR {
		t.Fatalf("expected Content-Range=%q, got %q", expectedCR, cr)
	}
}
