package auth

import (
	"context"
	"errors"
	"net/http"

	"github.com/ogen-go/ogen/ogenerrors"
)

// HTTPErrorHandler preserves 401 for invalid credentials but never asks clients
// to discard valid credentials because a startup dependency is unavailable.
func HTTPErrorHandler(ctx context.Context, w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, ErrUnavailable) {
		w.Header().Set("Retry-After", "30")
		http.Error(w, "authentication temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	ogenerrors.DefaultErrorHandler(ctx, w, r, err)
}
