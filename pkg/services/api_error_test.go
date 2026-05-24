package services

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/tgdrive/teldrive/internal/api"
	"github.com/tgdrive/teldrive/internal/apperr"
	"github.com/tgdrive/teldrive/internal/requestmeta"
	"github.com/tgdrive/teldrive/pkg/repositories"
)

func TestNewErrorAppError(t *testing.T) {
	service := &apiService{}
	fileID := uuid.New()
	err := fileNotFound(fileID, repositories.ErrNotFound)

	res, responseRequestID := newErrorWithRequestID(t, service, err, "req-123")

	assert.Equal(t, http.StatusNotFound, res.StatusCode)
	assert.Equal(t, http.StatusNotFound, res.Response.Code)
	assert.Equal(t, "file_not_found", res.Response.Error.Value)
	assert.Equal(t, "File not found", res.Response.Message)
	assert.Equal(t, "req-123", res.Response.RequestId.Value)
	assert.Equal(t, "req-123", responseRequestID)
}

func TestNewErrorFallbackNotFound(t *testing.T) {
	service := &apiService{}

	res, _ := newErrorWithRequestID(t, service, &apiError{err: repositories.ErrNotFound}, "")

	assert.Equal(t, http.StatusNotFound, res.StatusCode)
	assert.Equal(t, apperr.CodeNotFound, res.Response.Error.Value)
	assert.Equal(t, http.StatusText(http.StatusNotFound), res.Response.Message)
}

func newErrorWithRequestID(t *testing.T, service *apiService, err error, requestID string) (*api.ErrorStatusCode, string) {
	t.Helper()
	var res *api.ErrorStatusCode
	handler := requestmeta.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		res = service.NewError(r.Context(), err)
		w.WriteHeader(res.StatusCode)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if requestID != "" {
		req.Header.Set("X-Request-ID", requestID)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	return res, rec.Header().Get("X-Request-ID")
}
