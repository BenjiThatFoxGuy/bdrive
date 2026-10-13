package services

import (
	"bytes"
	"net/http"

	"go.uber.org/zap"

	"github.com/tgdrive/teldrive/internal/cbzbrowse"
	"github.com/tgdrive/teldrive/internal/logging"
	"github.com/tgdrive/teldrive/internal/zipbrowse"
)

// CbzBrowse lists the contents of a CBZ file at a given inner path.
// GET /files/cbz/{fileId}/list?path=/inner/path
func (e *extendedService) CbzBrowse(w http.ResponseWriter, r *http.Request, fileId string) {
	if !e.api.cnf.Files.EnableArchiveBrowsing || !e.api.cnf.Files.EnableCbzBrowsing {
		http.Error(w, "cbz browsing is disabled", http.StatusForbidden)
		return
	}

	ctx := r.Context()
	logger := logging.Component("CBZ").With(zap.String("file_id", fileId))

	session, userId, err := e.resolveZipSession(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	file, err := e.getFileForZip(ctx, fileId, userId)
	if err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}

	if file.Size == nil || *file.Size == 0 {
		zipbrowse.WriteJSON(w, http.StatusOK, &zipbrowse.ListResponse{Entries: []zipbrowse.Entry{}, Total: 0})
		return
	}

	if *file.Size > 500*1024*1024 {
		http.Error(w, "cbz too large for browsing (max 500 MB)", http.StatusRequestedRangeNotSatisfiable)
		return
	}

	innerPath := r.URL.Query().Get("path")

	buf, err := e.downloadFullFile(ctx, file, session, logger)
	if err != nil {
		logger.Error("cbz.download_failed", zap.Error(err))
		http.Error(w, "failed to download cbz", http.StatusInternalServerError)
		return
	}

	rdr := bytes.NewReader(buf.Bytes())
	listing, err := cbzbrowse.ListEntries(rdr, int64(buf.Len()), innerPath)
	if err != nil {
		logger.Error("cbz.list_failed", zap.Error(err))
		http.Error(w, "failed to read cbz: "+err.Error(), http.StatusInternalServerError)
		return
	}

	zipbrowse.WriteJSON(w, http.StatusOK, listing)
}

// CbzExtract serves a single file from inside a CBZ archive.
// GET /files/cbz/{fileId}/file?path=/inner/path/to/page.jpg
func (e *extendedService) CbzExtract(w http.ResponseWriter, r *http.Request, fileId string) {
	if !e.api.cnf.Files.EnableArchiveBrowsing || !e.api.cnf.Files.EnableCbzBrowsing {
		http.Error(w, "cbz browsing is disabled", http.StatusForbidden)
		return
	}

	ctx := r.Context()
	logger := logging.Component("CBZ").With(zap.String("file_id", fileId))

	session, userId, err := e.resolveZipSession(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	file, err := e.getFileForZip(ctx, fileId, userId)
	if err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}

	if file.Size == nil || *file.Size == 0 {
		http.Error(w, "empty file", http.StatusNotFound)
		return
	}

	if *file.Size > 500*1024*1024 {
		http.Error(w, "cbz too large for extraction (max 500 MB)", http.StatusRequestedRangeNotSatisfiable)
		return
	}

	innerPath := r.URL.Query().Get("path")
	if innerPath == "" {
		http.Error(w, "path parameter required", http.StatusBadRequest)
		return
	}

	buf, err := e.downloadFullFile(ctx, file, session, logger)
	if err != nil {
		logger.Error("cbz.download_failed", zap.Error(err))
		http.Error(w, "failed to download cbz", http.StatusInternalServerError)
		return
	}

	rdr := bytes.NewReader(buf.Bytes())
	if err := cbzbrowse.ExtractFile(w, rdr, int64(buf.Len()), innerPath); err != nil {
		logger.Error("cbz.extract_failed", zap.Error(err))
		http.Error(w, err.Error(), http.StatusNotFound)
	}
}

// CbzMetadata returns the parsed ComicInfo.xml metadata from a CBZ file.
// GET /files/cbz/{fileId}/metadata
func (e *extendedService) CbzMetadata(w http.ResponseWriter, r *http.Request, fileId string) {
	if !e.api.cnf.Files.EnableArchiveBrowsing || !e.api.cnf.Files.EnableCbzBrowsing {
		http.Error(w, "cbz browsing is disabled", http.StatusForbidden)
		return
	}

	ctx := r.Context()
	logger := logging.Component("CBZ").With(zap.String("file_id", fileId))

	session, userId, err := e.resolveZipSession(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	file, err := e.getFileForZip(ctx, fileId, userId)
	if err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}

	if file.Size == nil || *file.Size == 0 {
		zipbrowse.WriteJSON(w, http.StatusOK, &cbzbrowse.MetadataResponse{TotalPages: 0})
		return
	}

	if *file.Size > 500*1024*1024 {
		http.Error(w, "cbz too large for metadata extraction (max 500 MB)", http.StatusRequestedRangeNotSatisfiable)
		return
	}

	buf, err := e.downloadFullFile(ctx, file, session, logger)
	if err != nil {
		logger.Error("cbz.download_failed", zap.Error(err))
		http.Error(w, "failed to download cbz", http.StatusInternalServerError)
		return
	}

	rdr := bytes.NewReader(buf.Bytes())
	meta, err := cbzbrowse.GetMetadata(rdr, int64(buf.Len()))
	if err != nil {
		logger.Error("cbz.metadata_failed", zap.Error(err))
		http.Error(w, "failed to read cbz metadata: "+err.Error(), http.StatusInternalServerError)
		return
	}

	zipbrowse.WriteJSON(w, http.StatusOK, meta)
}

// CbzCover serves the cover image from a CBZ archive (first image by sort order).
// GET /files/cbz/{fileId}/cover
func (e *extendedService) CbzCover(w http.ResponseWriter, r *http.Request, fileId string) {
	if !e.api.cnf.Files.EnableArchiveBrowsing || !e.api.cnf.Files.EnableCbzBrowsing {
		http.Error(w, "cbz browsing is disabled", http.StatusForbidden)
		return
	}

	ctx := r.Context()
	logger := logging.Component("CBZ").With(zap.String("file_id", fileId))

	session, userId, err := e.resolveZipSession(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	file, err := e.getFileForZip(ctx, fileId, userId)
	if err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}

	if file.Size == nil || *file.Size == 0 {
		http.Error(w, "empty file", http.StatusNotFound)
		return
	}

	if *file.Size > 500*1024*1024 {
		http.Error(w, "cbz too large for cover extraction (max 500 MB)", http.StatusRequestedRangeNotSatisfiable)
		return
	}

	buf, err := e.downloadFullFile(ctx, file, session, logger)
	if err != nil {
		logger.Error("cbz.download_failed", zap.Error(err))
		http.Error(w, "failed to download cbz", http.StatusInternalServerError)
		return
	}

	rdr := bytes.NewReader(buf.Bytes())
	if err := cbzbrowse.ExtractCover(w, rdr, int64(buf.Len())); err != nil {
		logger.Error("cbz.cover_failed", zap.Error(err))
		http.Error(w, err.Error(), http.StatusNotFound)
	}
}
