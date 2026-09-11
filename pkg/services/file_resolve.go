package services

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/tgdrive/teldrive/internal/auth"
)

// resolveToken maps an ephemeral token to a file ID for the "show in folder"
// handshake. Tokens are single-use and expire after 60 seconds.
type resolveToken struct {
	fileID    string
	expiresAt time.Time
}

var (
	resolveTokens   = make(map[string]resolveToken)
	resolveTokensMu sync.Mutex
)

func init() {
	// Background cleanup of expired tokens every 30 seconds.
	go func() {
		for {
			time.Sleep(30 * time.Second)
			resolveTokensMu.Lock()
			now := time.Now()
			for k, v := range resolveTokens {
				if now.After(v.expiresAt) {
					delete(resolveTokens, k)
				}
			}
			resolveTokensMu.Unlock()
		}
	}()
}

// ResolveRouter returns a chi.Router that handles the resolve-token endpoints.
func (a *apiService) ResolveRouter() chi.Router {
	r := chi.NewRouter()

	// POST /api/files/resolve-token — create an ephemeral token for a file ID.
	// No auth required (called from share views).
	r.Post("/resolve-token", a.handleCreateResolveToken)

	return r
}

// ResolveHandler handles GET /resolve/{token}. If the user is authenticated,
// it resolves the token to the file's real path and redirects. If not, the
// SPA shows the login screen; after login the URL is revisited and resolves.
func (a *apiService) ResolveHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Prevent caching of resolve responses.
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("CDN-Cache-Control", "no-store")
		w.Header().Set("Cloudflare-CDN-Cache-Control", "no-store")

		token := chi.URLParam(r, "token")
		if token == "" {
			http.Error(w, "missing token", http.StatusBadRequest)
			return
		}

		userId := auth.GetUser(r.Context())
		if userId == 0 {
			// Not authenticated — let the SPA handle it (show login).
			// The SPA catch-all will serve index.html for this path.
			return
		}

		// Look up and consume the token.
		resolveTokensMu.Lock()
		tok, ok := resolveTokens[token]
		if ok {
			delete(resolveTokens, token)
		}
		resolveTokensMu.Unlock()

		if !ok || time.Now().After(tok.expiresAt) {
			http.Error(w, `{"error":"token expired or invalid"}`, http.StatusGone)
			return
		}

		// Resolve the file's full path.
		path, err := a.getFullPath(a.db, tok.fileID)
		if err != nil || path == "" {
			http.Error(w, `{"error":"file not found"}`, http.StatusNotFound)
			return
		}

		// Return the path as JSON so the frontend can navigate.
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"path": path})
	}
}

type createResolveTokenRequest struct {
	FileID string `json:"fileId"`
}

func (a *apiService) handleCreateResolveToken(w http.ResponseWriter, r *http.Request) {
	var req createResolveTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.FileID == "" {
		http.Error(w, `{"error":"fileId required"}`, http.StatusBadRequest)
		return
	}

	token := uuid.New().String()

	resolveTokensMu.Lock()
	resolveTokens[token] = resolveToken{
		fileID:    req.FileID,
		expiresAt: time.Now().Add(60 * time.Second),
	}
	resolveTokensMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"token": token})
}
