package apperr

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNotFound(t *testing.T) {
	cause := errors.New("repository: not found")
	err := NotFound("file", "abc", cause)

	assert.Equal(t, http.StatusNotFound, err.Status())
	assert.Equal(t, "file_not_found", err.Code())
	assert.Equal(t, "File not found", err.Message())
	assert.Equal(t, "file", err.Resource())
	assert.Equal(t, "abc", err.ID())
	assert.ErrorIs(t, err, cause)
}

func TestInternalDefaults(t *testing.T) {
	err := New(0, "", "", nil)

	assert.Equal(t, http.StatusInternalServerError, err.Status())
	assert.Equal(t, CodeInternal, err.Code())
	assert.Equal(t, http.StatusText(http.StatusInternalServerError), err.Message())
}
