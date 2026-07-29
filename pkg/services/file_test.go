package services

import (
	"strings"
	"testing"

	"github.com/tgdrive/teldrive/internal/api"
	"github.com/tgdrive/teldrive/pkg/models"
)

func TestBlockHashesCover(t *testing.T) {
	hashed := func(n int) []models.Upload {
		uploads := make([]models.Upload, n)
		for i := range uploads {
			uploads[i] = models.Upload{PartId: i + 1, BlockHashes: []byte{byte(i)}}
		}
		return uploads
	}
	parts := func(n int) []api.Part {
		out := make([]api.Part, n)
		for i := range out {
			out[i] = api.Part{ID: i + 1}
		}
		return out
	}

	tests := []struct {
		name    string
		uploads []models.Upload
		parts   []api.Part
		want    bool
	}{
		{
			name:    "every part has block hashes on record",
			uploads: hashed(3),
			parts:   parts(3),
			want:    true,
		},
		{
			name:    "no upload session to read hashes from",
			uploads: nil,
			parts:   parts(1),
			want:    false,
		},
		{
			name:    "client committed more parts than it uploaded",
			uploads: hashed(2),
			parts:   parts(3),
			want:    false,
		},
		{
			name:    "client committed fewer parts than it uploaded",
			uploads: hashed(3),
			parts:   parts(2),
			want:    false,
		},
		{
			name:    "one part uploaded with hashing disabled",
			uploads: append(hashed(2), models.Upload{PartId: 3}),
			parts:   parts(3),
			want:    false,
		},
		{
			name:    "hashing disabled for the whole upload",
			uploads: []models.Upload{{PartId: 1}},
			parts:   parts(1),
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := blockHashesCover(tt.uploads, tt.parts); got != tt.want {
				t.Errorf("blockHashesCover() = %v, want %v", got, tt.want)
			}
		})
	}
}

// The dedup hash backfill hashes a file that has not been inserted yet, so the
// nullable Encrypted flag has to be readable without a panic.
func TestFileIsEncrypted(t *testing.T) {
	yes, no := true, false
	tests := []struct {
		name string
		file models.File
		want bool
	}{
		{name: "unset flag reads as not encrypted", file: models.File{}, want: false},
		{name: "explicitly not encrypted", file: models.File{Encrypted: &no}, want: false},
		{name: "encrypted", file: models.File{Encrypted: &yes}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.file.IsEncrypted(); got != tt.want {
				t.Errorf("IsEncrypted() = %v, want %v", got, tt.want)
			}
		})
	}
}

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
