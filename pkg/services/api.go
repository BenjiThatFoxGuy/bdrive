package services

import (
	"context"
	"net/http"
	"time"

	"github.com/go-faster/errors"
	"github.com/ogen-go/ogen/ogenerrors"
	varc "github.com/tgdrive/varc/varc"
	"go.uber.org/zap"

	ht "github.com/ogen-go/ogen/http"
	"github.com/tgdrive/teldrive/internal/api"
	"github.com/tgdrive/teldrive/internal/apperr"
	"github.com/tgdrive/teldrive/internal/auth"
	"github.com/tgdrive/teldrive/internal/cache"
	"github.com/tgdrive/teldrive/internal/config"
	"github.com/tgdrive/teldrive/internal/events"
	"github.com/tgdrive/teldrive/internal/http_range"
	"github.com/tgdrive/teldrive/internal/logging"
	"github.com/tgdrive/teldrive/internal/requestmeta"
	"github.com/tgdrive/teldrive/internal/utils"
	"github.com/tgdrive/teldrive/internal/version"
	"github.com/tgdrive/teldrive/pkg/mapper"
	"github.com/tgdrive/teldrive/pkg/repositories"
	"github.com/tgdrive/teldrive/pkg/worker"
)

type apiService struct {
	cnf            *config.ServerCmdConfig
	cache          cache.Cacher
	varcCache      *varc.Cache
	events         events.EventBroadcaster
	authAttempts   *authAttemptManager
	channelManager ChannelManager
	telegram       TelegramService
	repo           *repositories.Repositories
	workerStore    *worker.Store
}

func (a *apiService) VersionVersion(ctx context.Context) (*api.ApiVersion, error) {
	return version.VersionInfo(), nil
}

func (a *apiService) EventsGetEvents(ctx context.Context) ([]api.Event, error) {
	//Get latest events within 5 minutes
	userId := auth.User(ctx)
	res, err := a.repo.Events.GetRecent(ctx, userId, time.Now().UTC().Add(-10*time.Minute), 100)
	if err != nil {
		return nil, &apiError{err: err}
	}
	return utils.Map(res, mapper.ToEventOut), nil
}

func (a *apiService) NewError(ctx context.Context, err error) *api.ErrorStatusCode {
	var (
		status      = http.StatusInternalServerError
		publicCode  = apperr.CodeInternal
		message     = http.StatusText(status)
		ogenErr     ogenerrors.Error
		appErr      *apperr.Error
		apiError    *apiError
		requestID   = requestmeta.RequestID(ctx)
		logCause    = err
		logWarnOnly bool
	)
	switch {
	case errors.Is(err, ht.ErrNotImplemented):
		status = http.StatusNotImplemented
		publicCode = "not_implemented"
		message = http.StatusText(status)
	case errors.As(err, &ogenErr):
		status = ogenErr.Code()
		publicCode = apperr.CodeBadRequest
		message = ogenErr.Error()
	case errors.As(err, &appErr):
		status = appErr.Status()
		publicCode = appErr.Code()
		message = appErr.Message()
	case errors.As(err, &apiError):
		if apiError.code == 0 {
			status, publicCode, message = fallbackErrorResponse(apiError.err)
		} else {
			status = apiError.code
			publicCode = fallbackCode(status)
			message = apiError.Error()
		}
		logCause = apiError.err
	default:
		status, publicCode, message = fallbackErrorResponse(err)
	}
	if status < http.StatusInternalServerError {
		logWarnOnly = true
	}
	fields := []zap.Field{
		zap.Int("status", status),
		zap.String("error_code", publicCode),
		zap.Error(logCause),
	}
	if requestID != "" {
		fields = append(fields, zap.String("request_id", requestID))
	}
	fields = append(fields, apperr.Fields(err)...)
	logger := logging.Component("API")
	if logWarnOnly {
		logger.Warn("request.failed", fields...)
	} else {
		logger.Error("request.failed", fields...)
	}

	res := api.Error{Code: status, Message: message, Error: api.NewOptString(publicCode)}
	if requestID != "" {
		res.RequestId = api.NewOptString(requestID)
	}
	return &api.ErrorStatusCode{StatusCode: status, Response: res}
}

func fallbackErrorResponse(err error) (int, string, string) {
	switch {
	case errors.Is(err, repositories.ErrNotFound):
		return http.StatusNotFound, apperr.CodeNotFound, http.StatusText(http.StatusNotFound)
	case errors.Is(err, repositories.ErrConflict):
		return http.StatusConflict, apperr.CodeConflict, http.StatusText(http.StatusConflict)
	case errors.Is(err, context.Canceled):
		return http.StatusRequestTimeout, apperr.CodeRequestTimeout, "Request canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout, apperr.CodeRequestTimeout, "Request timed out"
	case errors.Is(err, http_range.ErrNoOverlap):
		return http.StatusRequestedRangeNotSatisfiable, apperr.CodeInvalidRange, "Requested range is not satisfiable"
	default:
		return http.StatusInternalServerError, apperr.CodeInternal, http.StatusText(http.StatusInternalServerError)
	}
}

func fallbackCode(status int) string {
	switch status {
	case http.StatusBadRequest:
		return apperr.CodeBadRequest
	case http.StatusUnauthorized:
		return apperr.CodeUnauthorized
	case http.StatusForbidden:
		return apperr.CodeForbidden
	case http.StatusNotFound:
		return apperr.CodeNotFound
	case http.StatusConflict:
		return apperr.CodeConflict
	case http.StatusRequestedRangeNotSatisfiable:
		return apperr.CodeInvalidRange
	case http.StatusGatewayTimeout, http.StatusRequestTimeout:
		return apperr.CodeRequestTimeout
	default:
		if status >= http.StatusInternalServerError {
			return apperr.CodeInternal
		}
		return apperr.CodeBadRequest
	}
}

func NewApiService(repo *repositories.Repositories,
	channelManager ChannelManager,
	cnf *config.ServerCmdConfig,
	cache cache.Cacher,
	varcCache *varc.Cache,
	telegram TelegramService,
	events events.EventBroadcaster,
	workerStore *worker.Store) *apiService {

	return &apiService{
		repo:           repo,
		cnf:            cnf,
		cache:          cache,
		varcCache:      varcCache,
		events:         events,
		authAttempts:   newAuthAttemptManager(),
		channelManager: channelManager,
		telegram:       telegram,
		workerStore:    workerStore,
	}
}

type apiError struct {
	err  error
	code int
}

func (a apiError) Error() string {
	return a.err.Error()
}

func (a *apiError) Unwrap() error {
	return a.err
}

var (
	_ api.Handler = (*apiService)(nil)
	_ error       = apiError{}
)
