package services

import (
	"strings"
	"testing"
)

func TestContentTypeFor(t *testing.T) {
	tests := []struct {
		name     string
		fileName string
		mimeType string
		want     string
	}{
		{
			name:     "stored mime type wins",
			fileName: "clip.mp4",
			mimeType: "video/mp4",
			want:     "video/mp4",
		},
		{
			name:     "stored mime type wins even against the extension",
			fileName: "actually-a-png.jpg",
			mimeType: "image/png",
			want:     "image/png",
		},
		{
			name:     "empty mime type falls back to the extension",
			fileName: "boarding.pkpass",
			mimeType: "",
			want:     "application/vnd.apple.pkpass",
		},
		{
			name:     "generic mime type falls back to the extension",
			fileName: "boarding.pkpass",
			mimeType: defaultContentType,
			want:     "application/vnd.apple.pkpass",
		},
		{
			name:     "unknown extension keeps the default",
			fileName: "archive.somethingelse",
			mimeType: "",
			want:     defaultContentType,
		},
		{
			name:     "no extension keeps the default",
			fileName: "README",
			mimeType: "",
			want:     defaultContentType,
		},
		{
			name:     "extension match is case insensitive",
			fileName: "BOARDING.PKPASS",
			mimeType: "",
			want:     "application/vnd.apple.pkpass",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := contentTypeFor(tt.fileName, tt.mimeType)
			// Types resolved from the platform database may carry parameters
			// such as "; charset=utf-8", which callers don't care about.
			if base, _, _ := strings.Cut(got, ";"); strings.TrimSpace(base) != tt.want {
				t.Errorf("contentTypeFor(%q, %q) = %q, want %q", tt.fileName, tt.mimeType, got, tt.want)
			}
		})
	}
}
