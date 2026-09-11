// Package zipbrowse provides server-side browsing of zip file contents.
// It reads the zip central directory to list entries without extracting
// the entire archive, and can extract individual files on demand.
package zipbrowse

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Entry represents a single file or directory inside a zip archive.
type Entry struct {
	Name         string `json:"name"`
	Path         string `json:"path"`
	Size         int64  `json:"size"`
	CompressedSz int64  `json:"compressedSize"`
	IsDir        bool   `json:"isDir"`
	Modified     string `json:"modified,omitempty"`
	MimeType     string `json:"mimeType,omitempty"`
	ChildCount   int    `json:"childCount,omitempty"`
}

// ListResponse is returned by the list endpoint.
type ListResponse struct {
	Entries []Entry `json:"entries"`
	Total   int     `json:"total"`
	ZipPath string  `json:"zipPath"`
}

// ListEntries reads the zip central directory from r (which must be a
// ReaderAt of totalSize bytes) and returns the entries under innerPath.
// An empty innerPath lists the root of the zip.
func ListEntries(r io.ReaderAt, totalSize int64, innerPath string) (*ListResponse, error) {
	zr, err := zip.NewReader(r, totalSize)
	if err != nil {
		return nil, fmt.Errorf("open zip: %w", err)
	}

	innerPath = strings.TrimPrefix(innerPath, "/")
	if innerPath != "" && !strings.HasSuffix(innerPath, "/") {
		innerPath += "/"
	}

	// Collect unique direct children at innerPath depth.
	seen := map[string]bool{}
	var entries []Entry
	childCounts := map[string]int{}

	for _, f := range zr.File {
		name := f.Name
		if !strings.HasPrefix(name, innerPath) {
			continue
		}
		rel := strings.TrimPrefix(name, innerPath)
		if rel == "" {
			continue
		}

		// Direct child: no slash, or exactly one trailing slash (directory).
		parts := strings.SplitN(rel, "/", 2)
		childName := parts[0]

		if len(parts) == 2 && parts[1] != "" {
			// This is a deeper entry — count it toward the child directory.
			childCounts[childName]++
			if seen[childName] {
				continue
			}
			seen[childName] = true
			entries = append(entries, Entry{
				Name:     childName,
				Path:     innerPath + childName,
				IsDir:    true,
				Modified: f.Modified.UTC().Format(time.RFC3339),
			})
			continue
		}

		if seen[childName] {
			continue
		}
		seen[childName] = true

		isDir := strings.HasSuffix(name, "/")
		e := Entry{
			Name:         childName,
			Path:         strings.TrimSuffix(innerPath+childName, "/"),
			Size:         int64(f.UncompressedSize64),
			CompressedSz: int64(f.CompressedSize64),
			IsDir:        isDir,
			Modified:     f.Modified.UTC().Format(time.RFC3339),
		}
		if !isDir {
			e.MimeType = mimeFromName(childName)
		}
		entries = append(entries, e)
	}

	// Fill in child counts for directories.
	for i := range entries {
		if entries[i].IsDir {
			entries[i].ChildCount = childCounts[entries[i].Name]
		}
	}

	// Sort: directories first, then by name.
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})

	return &ListResponse{
		Entries: entries,
		Total:   len(entries),
		ZipPath: innerPath,
	}, nil
}

// ExtractFile finds the entry at innerPath inside the zip and copies it
// to w with appropriate Content-Type and Content-Disposition headers.
func ExtractFile(w http.ResponseWriter, r io.ReaderAt, totalSize int64, innerPath string) error {
	zr, err := zip.NewReader(r, totalSize)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}

	innerPath = strings.TrimPrefix(innerPath, "/")

	for _, f := range zr.File {
		name := strings.TrimSuffix(f.Name, "/")
		if name != innerPath {
			continue
		}
		if f.FileInfo().IsDir() {
			return fmt.Errorf("path is a directory")
		}
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("open entry: %w", err)
		}
		defer rc.Close()

		ct := mimeFromName(f.Name)
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Content-Length", fmt.Sprintf("%d", f.UncompressedSize64))
		disp := mime.FormatMediaType("inline", map[string]string{
			"filename": path.Base(f.Name),
		})
		w.Header().Set("Content-Disposition", disp)
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.WriteHeader(http.StatusOK)
		io.Copy(w, rc)
		return nil
	}

	return fmt.Errorf("entry not found: %s", innerPath)
}

// WriteJSON is a small helper to write a JSON response.
func WriteJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func mimeFromName(name string) string {
	ext := filepath.Ext(name)
	if ext == "" {
		return "application/octet-stream"
	}
	ct := mime.TypeByExtension(ext)
	if ct == "" {
		return "application/octet-stream"
	}
	return ct
}
