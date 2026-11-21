package thumbnail

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// Source serves cached thumbnail bytes over HTTP, supporting Range and HEAD
// requests so that imgproxy (or any HTTP client) can fetch partial content.
type Source struct {
	size      int64
	openRange func(ctx context.Context, start, end int64) (io.ReadCloser, error)
}

// NewSource creates a Source backed by size bytes readable via openRange.
// openRange(ctx, start, end) must return the bytes in [start, end] inclusive.
func NewSource(size int64, openRange func(ctx context.Context, start, end int64) (io.ReadCloser, error)) *Source {
	return &Source{size: size, openRange: openRange}
}

// ServeHTTP implements http.Handler. It supports GET with optional Range
// headers and HEAD without reading the backing store.
func (s *Source) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Type", "image/jpeg")

	if r.Method == http.MethodHead {
		w.Header().Set("Content-Length", strconv.FormatInt(s.size, 10))
		w.WriteHeader(http.StatusOK)
		return
	}

	rangeHeader := r.Header.Get("Range")
	if rangeHeader == "" {
		// Full content.
		w.Header().Set("Content-Length", strconv.FormatInt(s.size, 10))
		rc, err := s.openRange(r.Context(), 0, s.size-1)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rc.Close()
		w.WriteHeader(http.StatusOK)
		io.Copy(w, rc)
		return
	}

	// Parse Range header.
	start, end, ok := parseRange(rangeHeader, s.size)
	if !ok {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", s.size))
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	}

	contentLength := end - start + 1
	w.Header().Set("Content-Length", strconv.FormatInt(contentLength, 10))
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, s.size))

	rc, err := s.openRange(r.Context(), start, end)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rc.Close()

	w.WriteHeader(http.StatusPartialContent)
	io.Copy(w, rc)
}

// parseRange extracts the first byte range from a Range header value.
func parseRange(header string, size int64) (start, end int64, ok bool) {
	if !strings.HasPrefix(header, "bytes=") {
		return 0, 0, false
	}
	spec := strings.TrimPrefix(header, "bytes=")
	parts := strings.SplitN(spec, "-", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}

	if parts[0] == "" {
		// Suffix range: bytes=-N
		n, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || n <= 0 {
			return 0, 0, false
		}
		start = size - n
		if start < 0 {
			start = 0
		}
		end = size - 1
	} else {
		var err error
		start, err = strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return 0, 0, false
		}
		if parts[1] == "" {
			end = size - 1
		} else {
			end, err = strconv.ParseInt(parts[1], 10, 64)
			if err != nil {
				return 0, 0, false
			}
		}
	}

	if start < 0 || start >= size || end >= size || start > end {
		return 0, 0, false
	}
	return start, end, true
}
