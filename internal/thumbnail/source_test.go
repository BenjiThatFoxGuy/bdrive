package thumbnail

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSourceServesSeekRanges(t *testing.T) {
	data := []byte("0123456789")
	var gotStart, gotEnd int64
	h := NewSource(int64(len(data)), func(ctx context.Context, start, end int64) (io.ReadCloser, error) {
		gotStart, gotEnd = start, end
		return io.NopCloser(bytes.NewReader(data[start : end+1])), nil
	})
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Range", "bytes=6-8")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusPartialContent || w.Body.String() != "678" {
		t.Fatalf("range response: status=%d body=%q", w.Code, w.Body.String())
	}
	if gotStart != 6 || gotEnd != 8 || w.Header().Get("Content-Range") != "bytes 6-8/10" {
		t.Fatalf("range boundaries: %d-%d, headers=%v", gotStart, gotEnd, w.Header())
	}
}

func TestSourceRejectsOutOfBoundsRange(t *testing.T) {
	h := NewSource(10, func(context.Context, int64, int64) (io.ReadCloser, error) {
		t.Fatal("invalid range opened the source")
		return nil, nil
	})
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Range", "bytes=10-")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("status=%d, want 416", w.Code)
	}
}

func TestSourceHeadDoesNotReadTelegram(t *testing.T) {
	h := NewSource(10, func(context.Context, int64, int64) (io.ReadCloser, error) {
		t.Fatal("HEAD opened the source")
		return nil, nil
	})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodHead, "/", nil))
	if w.Code != http.StatusOK || w.Header().Get("Content-Length") != "10" || w.Body.Len() != 0 {
		t.Fatalf("HEAD response: status=%d headers=%v body=%q", w.Code, w.Header(), w.Body.String())
	}
}
