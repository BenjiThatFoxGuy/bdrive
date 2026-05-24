package apperr

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"go.uber.org/zap"
)

const (
	CodeBadRequest     = "bad_request"
	CodeConflict       = "conflict"
	CodeForbidden      = "forbidden"
	CodeInternal       = "internal_error"
	CodeInvalidRange   = "invalid_range"
	CodeNotFound       = "not_found"
	CodeRequestTimeout = "request_timeout"
	CodeUnauthorized   = "unauthorized"
)

type Error struct {
	status   int
	code     string
	message  string
	resource string
	id       string
	cause    error
}

func New(status int, code, message string, cause error) *Error {
	if status == 0 {
		status = http.StatusInternalServerError
	}
	if code == "" {
		code = CodeInternal
	}
	if message == "" {
		message = http.StatusText(status)
	}
	return &Error{status: status, code: code, message: message, cause: cause}
}

func BadRequest(code, message string, cause error) *Error {
	return New(http.StatusBadRequest, code, message, cause)
}

func Conflict(code, message string, cause error) *Error {
	return New(http.StatusConflict, code, message, cause)
}

func Forbidden(code, message string, cause error) *Error {
	return New(http.StatusForbidden, code, message, cause)
}

func Internal(code, message string, cause error) *Error {
	return New(http.StatusInternalServerError, code, message, cause)
}

func InvalidRange(message string, cause error) *Error {
	return New(http.StatusRequestedRangeNotSatisfiable, CodeInvalidRange, message, cause)
}

func NotFound(resource, id string, cause error) *Error {
	code := CodeNotFound
	message := http.StatusText(http.StatusNotFound)
	if resource != "" {
		code = resource + "_not_found"
		message = title(resource) + " not found"
	}
	return New(http.StatusNotFound, code, message, cause).WithResource(resource, id)
}

func RequestTimeout(cause error) *Error {
	return New(http.StatusGatewayTimeout, CodeRequestTimeout, "Request timed out", cause)
}

func Unauthorized(code, message string, cause error) *Error {
	return New(http.StatusUnauthorized, code, message, cause)
}

func (e *Error) WithResource(resource, id string) *Error {
	e.resource = resource
	e.id = id
	return e
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.cause == nil {
		return e.message
	}
	if e.resource != "" && e.id != "" {
		return fmt.Sprintf("%s %s: %v", e.resource, e.id, e.cause)
	}
	return e.cause.Error()
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func (e *Error) Status() int {
	return e.status
}

func (e *Error) Code() string {
	return e.code
}

func (e *Error) Message() string {
	return e.message
}

func (e *Error) Resource() string {
	return e.resource
}

func (e *Error) ID() string {
	return e.id
}

func Fields(err error) []zap.Field {
	var appErr *Error
	if !errors.As(err, &appErr) || appErr == nil {
		return nil
	}
	fields := []zap.Field{
		zap.Int("status", appErr.Status()),
		zap.String("error_code", appErr.Code()),
	}
	if appErr.Resource() != "" {
		fields = append(fields, zap.String("resource", appErr.Resource()))
	}
	if appErr.ID() != "" {
		fields = append(fields, zap.String("resource_id", appErr.ID()))
	}
	return fields
}

func title(value string) string {
	if value == "" {
		return value
	}
	value = strings.ReplaceAll(value, "_", " ")
	value = strings.ReplaceAll(value, "-", " ")
	return strings.ToUpper(value[:1]) + value[1:]
}
