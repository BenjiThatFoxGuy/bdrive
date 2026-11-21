package services

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEdgeCacheable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		query string
		want  bool
	}{
		{"no credentials in the url", "", false},
		{"share hash in the url", "?hash=abc123", true},
		{"ephemeral token in the url", "?token=xyz", true},
		{"unrelated query parameter", "?download=1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := httptest.NewRequest(http.MethodGet, "/api/files/1"+tt.query, nil)
			if got := edgeCacheable(r); got != tt.want {
				t.Fatalf("edgeCacheable(%q) = %v, want %v", tt.query, got, tt.want)
			}
		})
	}
}

func TestStreamEdgeCacheSeconds(t *testing.T) {
	t.Parallel()

	withHash := httptest.NewRequest(http.MethodGet, "/api/files/1?hash=abc", nil)
	if got := streamEdgeCacheSeconds(withHash); got != edgeCacheSeconds {
		t.Fatalf("tokenised request = %d, want %d", got, edgeCacheSeconds)
	}

	withCookie := httptest.NewRequest(http.MethodGet, "/api/files/1", nil)
	withCookie.AddCookie(&http.Cookie{Name: authCookieName, Value: "session"})
	if got := streamEdgeCacheSeconds(withCookie); got != 0 {
		t.Fatalf("cookie-authenticated request = %d, want 0", got)
	}
}

func TestSetEdgeCache(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	setEdgeCache(rec, edgeCacheSeconds)
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=86400, immutable" {
		t.Fatalf("Cache-Control = %q", got)
	}
	if got := rec.Header().Get("CDN-Cache-Control"); got != "public, max-age=86400, immutable" {
		t.Fatalf("CDN-Cache-Control = %q", got)
	}

	rec = httptest.NewRecorder()
	setEdgeCache(rec, 0)
	if got := rec.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("Cache-Control = %q", got)
	}
	if got := rec.Header().Get("CDN-Cache-Control"); got != "no-store" {
		t.Fatalf("CDN-Cache-Control = %q", got)
	}
}
