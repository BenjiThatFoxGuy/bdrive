package services

import (
	"bytes"
	"net/http"

	"go.uber.org/zap"

	"github.com/tgdrive/teldrive/internal/logging"
	"github.com/tgdrive/teldrive/internal/unitypkgbrowse"
	"github.com/tgdrive/teldrive/internal/zipbrowse"
)

// UnityPkgBrowse lists the contents of a .unitypackage file at a given inner path.
// GET /files/unitypkg/{fileId}/list?path=/inner/path
func (e *extendedService) UnityPkgBrowse(w http.ResponseWriter, r *http.Request, fileId string) {
	if !e.api.cnf.Files.EnableZipBrowsing {
		http.Error(w, "archive browsing is disabled", http.StatusForbidden)
		return
	}

	ctx := r.Context()
	logger := logging.Component("UNITYPKG").With(zap.String("file_id", fileId))

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
		http.Error(w, "unitypackage too large for browsing (max 500 MB)", http.StatusRequestedRangeNotSatisfiable)
		return
	}

	innerPath := r.URL.Query().Get("path")

	buf, err := e.downloadFullFile(ctx, file, session, logger)
	if err != nil {
		logger.Error("unitypkg.download_failed", zap.Error(err))
		http.Error(w, "failed to download unitypackage", http.StatusInternalServerError)
		return
	}

	listing, err := unitypkgbrowse.ListEntries(buf.Bytes(), innerPath)
	if err != nil {
		logger.Error("unitypkg.list_failed", zap.Error(err))
		http.Error(w, "failed to read unitypackage: "+err.Error(), http.StatusInternalServerError)
		return
	}

	zipbrowse.WriteJSON(w, http.StatusOK, listing)
}

// UnityPkgExtract serves a single file from inside a .unitypackage archive.
// GET /files/unitypkg/{fileId}/file?path=/inner/path/to/file.txt
func (e *extendedService) UnityPkgExtract(w http.ResponseWriter, r *http.Request, fileId string) {
	if !e.api.cnf.Files.EnableZipBrowsing {
		http.Error(w, "archive browsing is disabled", http.StatusForbidden)
		return
	}

	ctx := r.Context()
	logger := logging.Component("UNITYPKG").With(zap.String("file_id", fileId))

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
		http.Error(w, "unitypackage too large for extraction (max 500 MB)", http.StatusRequestedRangeNotSatisfiable)
		return
	}

	innerPath := r.URL.Query().Get("path")
	if innerPath == "" {
		http.Error(w, "path parameter required", http.StatusBadRequest)
		return
	}

	buf, err := e.downloadFullFile(ctx, file, session, logger)
	if err != nil {
		logger.Error("unitypkg.download_failed", zap.Error(err))
		http.Error(w, "failed to download unitypackage", http.StatusInternalServerError)
		return
	}

	var out bytes.Buffer
	if err := unitypkgbrowse.ExtractFile(&out, buf.Bytes(), innerPath); err != nil {
		logger.Error("unitypkg.extract_failed", zap.Error(err))
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Write(out.Bytes())
}
