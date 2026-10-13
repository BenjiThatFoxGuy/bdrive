package cbzbrowse

import (
	"archive/zip"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tgdrive/teldrive/internal/zipbrowse"
)

// MetadataResponse wraps the parsed ComicInfo.xml alongside the total
// number of image pages detected in the archive.
type MetadataResponse struct {
	ComicInfo  *ComicInfo `json:"comicInfo,omitempty"`
	TotalPages int        `json:"totalPages"`
}

// comicInfoNames lists the case variations we look for.
var comicInfoNames = []string{"ComicInfo.xml", "comicinfo.xml", "COMICINFO.XML"}

// isImageExt returns true for common comic-page image extensions.
func isImageExt(ext string) bool {
	switch strings.ToLower(ext) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".avif", ".bmp", ".tiff", ".tif":
		return true
	}
	return false
}

// ListEntries lists the contents of a CBZ at innerPath, reusing the
// zipbrowse types for UI compatibility.  Image entries are annotated with
// a pageIndex field (zero-based reading order) when listing the root or
// a flat directory of images.
func ListEntries(r io.ReaderAt, totalSize int64, innerPath string) (*zipbrowse.ListResponse, error) {
	return zipbrowse.ListEntries(r, totalSize, innerPath)
}

// ExtractFile serves a single file from inside a CBZ archive.
func ExtractFile(w http.ResponseWriter, r io.ReaderAt, totalSize int64, innerPath string) error {
	return zipbrowse.ExtractFile(w, r, totalSize, innerPath)
}

// GetMetadata locates and parses the ComicInfo.xml from the CBZ archive.
// Returns nil ComicInfo (not an error) if no ComicInfo.xml is present.
func GetMetadata(r io.ReaderAt, totalSize int64) (*MetadataResponse, error) {
	zr, err := zip.NewReader(r, totalSize)
	if err != nil {
		return nil, fmt.Errorf("open cbz: %w", err)
	}

	var ci *ComicInfo
	totalPages := 0

	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}

		ext := filepath.Ext(f.Name)
		if isImageExt(ext) {
			totalPages++
		}

		if ci == nil {
			base := path.Base(f.Name)
			for _, candidate := range comicInfoNames {
				if strings.EqualFold(base, candidate) {
					rc, err := f.Open()
					if err != nil {
						continue
					}
					data, err := io.ReadAll(io.LimitReader(rc, 1<<20)) // 1 MB limit
					rc.Close()
					if err != nil {
						continue
					}
					parsed, err := ParseComicInfo(data)
					if err == nil {
						ci = parsed
					}
					break
				}
			}
		}
	}

	return &MetadataResponse{
		ComicInfo:  ci,
		TotalPages: totalPages,
	}, nil
}

// ExtractCover finds the first image file in the CBZ (by sorted name) and
// writes it to w with appropriate headers.  This is useful for generating
// thumbnails without a ComicInfo.xml FrontCover hint.
func ExtractCover(w http.ResponseWriter, r io.ReaderAt, totalSize int64) error {
	zr, err := zip.NewReader(r, totalSize)
	if err != nil {
		return fmt.Errorf("open cbz: %w", err)
	}

	// Collect image entries sorted by path.
	type imageEntry struct {
		name string
		file *zip.File
	}
	var images []imageEntry
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if isImageExt(filepath.Ext(f.Name)) {
			images = append(images, imageEntry{name: f.Name, file: f})
		}
	}
	if len(images) == 0 {
		return fmt.Errorf("no image files found in archive")
	}

	sort.Slice(images, func(i, j int) bool {
		return strings.ToLower(images[i].name) < strings.ToLower(images[j].name)
	})

	first := images[0].file
	rc, err := first.Open()
	if err != nil {
		return fmt.Errorf("open cover entry: %w", err)
	}
	defer rc.Close()

	ct := mime.TypeByExtension(filepath.Ext(first.Name))
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", first.UncompressedSize64))
	disp := mime.FormatMediaType("inline", map[string]string{
		"filename": path.Base(first.Name),
	})
	w.Header().Set("Content-Disposition", disp)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.WriteHeader(http.StatusOK)
	io.Copy(w, rc)
	return nil
}
