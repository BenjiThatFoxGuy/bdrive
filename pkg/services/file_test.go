package services

import (
	"archive/zip"
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

func TestPathWithinShare(t *testing.T) {
	tests := []struct {
		name      string
		sharePath string
		filePath  string
		want      bool
	}{
		{name: "the shared folder itself", sharePath: "/Photos", filePath: "/Photos", want: true},
		{name: "a direct child", sharePath: "/Photos", filePath: "/Photos/cat.jpg", want: true},
		{name: "a deep descendant", sharePath: "/Photos", filePath: "/Photos/2024/jan/cat.jpg", want: true},
		{name: "a sibling sharing a name prefix", sharePath: "/Photos", filePath: "/PhotosBackup/cat.jpg", want: false},
		{name: "a sibling one level up", sharePath: "/Photos/2024", filePath: "/Photos/2023/cat.jpg", want: false},
		{name: "an unrelated path", sharePath: "/Photos", filePath: "/Documents/tax.pdf", want: false},
		{name: "a parent of the share", sharePath: "/Photos/2024", filePath: "/Photos", want: false},
		{name: "a root share covers everything", sharePath: "", filePath: "/Documents/tax.pdf", want: true},
		{name: "a trailing slash doesn't change the boundary", sharePath: "/Photos/", filePath: "/PhotosBackup", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pathWithinShare(tt.sharePath, tt.filePath); got != tt.want {
				t.Errorf("pathWithinShare(%q, %q) = %v, want %v", tt.sharePath, tt.filePath, got, tt.want)
			}
		})
	}
}

func TestCheckZipLimits(t *testing.T) {
	sized := func(sizes ...int64) []models.File {
		files := make([]models.File, len(sizes))
		for i := range sizes {
			size := sizes[i]
			files[i] = models.File{Size: &size}
		}
		return files
	}

	tests := []struct {
		name     string
		files    []models.File
		maxFiles int
		maxSize  int64
		wantErr  bool
	}{
		{name: "under both limits", files: sized(1, 2, 3), maxFiles: 10, maxSize: 100},
		{name: "exactly at the file limit", files: sized(1, 2, 3), maxFiles: 3, maxSize: 0},
		{name: "over the file limit", files: sized(1, 2, 3, 4), maxFiles: 3, maxSize: 0, wantErr: true},
		{name: "exactly at the size limit", files: sized(40, 60), maxFiles: 0, maxSize: 100},
		{name: "over the size limit", files: sized(40, 61), maxFiles: 0, maxSize: 100, wantErr: true},
		{name: "zero disables both limits", files: sized(1, 2, 3, 4, 5), maxFiles: 0, maxSize: 0},
		{name: "files without a size don't count toward the total", files: append(sized(10), models.File{}), maxFiles: 0, maxSize: 10},
		{name: "no files", files: nil, maxFiles: 1, maxSize: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkZipLimits(tt.files, tt.maxFiles, tt.maxSize)
			if (err != nil) != tt.wantErr {
				t.Errorf("checkZipLimits() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestZipCompressionMethod(t *testing.T) {
	tests := []struct {
		name     string
		fileName string
		want     uint16
	}{
		{name: "video is already compressed", fileName: "clip.mp4", want: zip.Store},
		{name: "image is already compressed", fileName: "cat.jpg", want: zip.Store},
		{name: "audio is already compressed", fileName: "song.mp3", want: zip.Store},
		{name: "archive is already compressed", fileName: "backup.zip", want: zip.Store},
		{name: "uppercase extensions are recognised", fileName: "CLIP.MP4", want: zip.Store},
		{name: "text compresses well", fileName: "notes.txt", want: zip.Deflate},
		{name: "documents compress well enough", fileName: "tax.pdf", want: zip.Deflate},
		{name: "unknown extension defaults to deflate", fileName: "data.bin", want: zip.Deflate},
		{name: "no extension defaults to deflate", fileName: "README", want: zip.Deflate},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := zipCompressionMethod(tt.fileName); got != tt.want {
				t.Errorf("zipCompressionMethod(%q) = %v, want %v", tt.fileName, got, tt.want)
			}
		})
	}
}

func TestShareZipName(t *testing.T) {
	folder := &fileShare{Type: api.FileShareInfoTypeFolder, Name: "Holiday"}
	folder.FileId = "folder-id"
	file := &fileShare{Type: api.FileShareInfoTypeFile, Name: "invoice.pdf"}
	file.FileId = "file-id"

	tests := []struct {
		name          string
		share         *fileShare
		ids           []string
		wantName      string
		wantNestUnder bool
	}{
		{
			name:          "the whole shared folder is named after the share",
			share:         folder,
			ids:           []string{"folder-id"},
			wantName:      "Holiday.zip",
			wantNestUnder: true,
		},
		{
			name:     "a selection inside the share stays generic",
			share:    folder,
			ids:      []string{"child-a", "child-b"},
			wantName: "download.zip",
		},
		{
			name:     "a single child does not take the share's name",
			share:    folder,
			ids:      []string{"child-a"},
			wantName: "download.zip",
		},
		{
			name:     "a shared file is named after the share, not nested",
			share:    file,
			ids:      []string{"file-id"},
			wantName: "invoice.pdf.zip",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotName, gotNest := shareZipName(tt.share, tt.ids)
			if gotName != tt.wantName || gotNest != tt.wantNestUnder {
				t.Errorf("shareZipName() = (%q, %v), want (%q, %v)", gotName, gotNest, tt.wantName, tt.wantNestUnder)
			}
		})
	}
}
