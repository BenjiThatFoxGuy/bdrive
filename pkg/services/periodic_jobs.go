package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-faster/jx"
	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
	"github.com/tgdrive/teldrive/internal/api"
	"github.com/tgdrive/teldrive/internal/apperr"
	"github.com/tgdrive/teldrive/internal/auth"
	internalduration "github.com/tgdrive/teldrive/internal/duration"
	"github.com/tgdrive/teldrive/pkg/repositories"
)

const (
	periodicJobKindCleanOldEvents    = "clean.old_events"
	periodicJobKindCleanStaleUpload  = "clean.stale_uploads"
	periodicJobKindCleanPendingFile  = "clean.pending_files"
	periodicJobKindRefreshFolderSize = "refresh.folder_sizes"
	defaultOldEventsRetention        = "5d"
	defaultStaleUploadRetention      = "1d"
)

type periodicJobRow struct {
	ID             string
	UserID         int64
	Name           string
	Kind           string
	Args           repositories.PeriodicJobArgs
	CronExpression string
	Enabled        bool
	System         bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type periodicJobPreset struct {
	Name           string
	Kind           string
	CronExpression string
	Args           repositories.PeriodicJobArgs
	System         bool
}

func (a *apiService) PeriodicJobsList(ctx context.Context) ([]api.PeriodicJobSummary, error) {
	userID := auth.User(ctx)
	if err := a.ensureDefaultPeriodicJobs(ctx, userID); err != nil {
		return nil, err
	}

	rows, err := a.repo.PeriodicJobs.ListByUserID(ctx, userID)
	if err != nil {
		return nil, &apiError{err: err}
	}

	out := make([]api.PeriodicJobSummary, 0, len(rows))
	for _, itemRow := range rows {
		item, err := toAPIPeriodicJobSummary(fromPeriodicJobModel(itemRow))
		if err != nil {
			return nil, &apiError{err: err}
		}
		out = append(out, *item)
	}
	return out, nil
}

func (a *apiService) PeriodicJobsGet(ctx context.Context, params api.PeriodicJobsGetParams) (*api.PeriodicJobDetail, error) {
	row, err := a.getPeriodicJobRow(ctx, uuid.UUID(params.ID).String(), auth.User(ctx))
	if err != nil {
		return nil, err
	}
	item, convErr := toAPIPeriodicJobDetail(row)
	if convErr != nil {
		return nil, &apiError{err: convErr}
	}
	return item, nil
}

func (a *apiService) PeriodicJobsUpdate(ctx context.Context, req *api.PeriodicJobUpdate, params api.PeriodicJobsUpdateParams) (*api.PeriodicJobDetail, error) {
	row, err := a.getPeriodicJobRow(ctx, uuid.UUID(params.ID).String(), auth.User(ctx))
	if err != nil {
		return nil, err
	}

	name := row.Name
	if req.Name.IsSet() {
		if row.System {
			return nil, &apiError{err: errors.New("system job name cannot be changed"), code: 400}
		}
		name = req.Name.Value
	}

	cronExpr := row.CronExpression
	if req.CronExpression.IsSet() {
		cronExpr = strings.TrimSpace(req.CronExpression.Value)
	}
	if err := validatePeriodicCron(cronExpr); err != nil {
		return nil, &apiError{err: err, code: 400}
	}

	enabled := row.Enabled
	if req.Enabled.IsSet() {
		enabled = req.Enabled.Value
	}

	updatedArgs := row.Args
	if req.Args.IsSet() {
		updatedArgs = updatePeriodicJobArgsFromAPI(row.Kind, row.Args, req.Args.Value)
	}

	err = a.repo.PeriodicJobs.Update(ctx, uuid.MustParse(row.ID), row.UserID, repositories.PeriodicJob{
		Kind:           row.Kind,
		Name:           name,
		CronExpression: cronExpr,
		Enabled:        enabled,
		Args:           updatedArgs,
		UpdatedAt:      time.Now().UTC(),
	})
	if err != nil {
		return nil, &apiError{err: err}
	}

	return a.PeriodicJobsGet(ctx, api.PeriodicJobsGetParams(params))
}

func updatePeriodicJobArgsFromAPI(kind string, current repositories.PeriodicJobArgs, update api.PeriodicJobUpdateArgs) repositories.PeriodicJobArgs {
	// For maintenance jobs, merge update into current args via JSON round-trip
	b, err := json.Marshal(update)
	if err != nil {
		return current
	}

	switch kind {
	case periodicJobKindCleanOldEvents:
		var args repositories.CleanOldEventsPeriodicArgs
		if current != nil {
			b, _ = json.Marshal(current)
		}
		if err := json.Unmarshal(b, &args); err == nil {
			if normalized, ok := normalizeRetentionString(args.Retention); ok {
				return repositories.CleanOldEventsPeriodicArgs{Retention: normalized}
			}
		}
		return current
	case periodicJobKindCleanStaleUpload:
		var args repositories.CleanStaleUploadsPeriodicArgs
		if err := json.Unmarshal(b, &args); err == nil {
			if normalized, ok := normalizeRetentionString(args.Retention); ok {
				return repositories.CleanStaleUploadsPeriodicArgs{Retention: normalized}
			}
		}
		return current
	default:
		return current
	}
}

func (a *apiService) PeriodicJobsDelete(ctx context.Context, params api.PeriodicJobsDeleteParams) error {
	row, err := a.getPeriodicJobRow(ctx, uuid.UUID(params.ID).String(), auth.User(ctx))
	if err != nil {
		return err
	}
	if row.System {
		return &apiError{err: errors.New("system jobs cannot be deleted"), code: 400}
	}
	err = a.repo.PeriodicJobs.Delete(ctx, uuid.MustParse(row.ID), row.UserID)
	if err != nil {
		return &apiError{err: err}
	}
	return nil
}

func (a *apiService) PeriodicJobsEnable(ctx context.Context, params api.PeriodicJobsEnableParams) error {
	return a.setPeriodicJobEnabled(ctx, uuid.UUID(params.ID).String(), true)
}

func (a *apiService) PeriodicJobsDisable(ctx context.Context, params api.PeriodicJobsDisableParams) error {
	return a.setPeriodicJobEnabled(ctx, uuid.UUID(params.ID).String(), false)
}

func (a *apiService) PeriodicJobsRun(ctx context.Context, params api.PeriodicJobsRunParams) (*api.PeriodicJobRunStatus, error) {
	row, err := a.getPeriodicJobRow(ctx, uuid.UUID(params.ID).String(), auth.User(ctx))
	if err != nil {
		return nil, err
	}
	if a.workerStore == nil {
		return nil, &apiError{err: errors.New("background job service is not configured"), code: 503}
	}
	if err := a.workerStore.MarkDueNow(ctx, uuid.MustParse(row.ID), row.UserID); err != nil {
		return nil, &apiError{err: err}
	}
	return &api.PeriodicJobRunStatus{
		ID:        api.UUID(uuid.MustParse(row.ID)),
		Kind:      api.PeriodicJobKind(row.Kind),
		State:     api.PeriodicJobRunStateScheduled,
		CreatedAt: row.CreatedAt,
	}, nil
}

func (a *apiService) setPeriodicJobEnabled(ctx context.Context, id string, enabled bool) error {
	row, err := a.getPeriodicJobRow(ctx, id, auth.User(ctx))
	if err != nil {
		return err
	}
	err = a.repo.PeriodicJobs.SetEnabled(ctx, uuid.MustParse(row.ID), row.UserID, enabled, time.Now().UTC())
	if err != nil {
		return &apiError{err: err}
	}
	return nil
}

func (a *apiService) ensureDefaultPeriodicJobs(ctx context.Context, userID int64) error {
	if userID == 0 {
		return nil
	}
	for _, preset := range defaultPeriodicJobPresets() {
		row, err := a.getPeriodicJobByName(ctx, userID, preset.Name)
		if err != nil {
			var appErr *apperr.Error
			if errors.As(err, &appErr) && appErr.Status() == http.StatusNotFound {
				_, createErr := a.insertPeriodicPreset(ctx, userID, preset)
				if createErr != nil {
					return createErr
				}
				continue
			}
			return err
		}
		if updated, updateErr := a.ensurePeriodicPresetArgs(ctx, row); updateErr != nil {
			return updateErr
		} else if updated != nil {
			_ = updated
		}
	}
	return nil
}

func defaultPeriodicJobPresets() []periodicJobPreset {
	return []periodicJobPreset{
		{Name: "Clean Old Events", Kind: periodicJobKindCleanOldEvents, CronExpression: "0 */12 * * *", Args: defaultCleanOldEventsPeriodicArgs(), System: true},
		{Name: "Clean Stale Uploads", Kind: periodicJobKindCleanStaleUpload, CronExpression: "0 */12 * * *", Args: defaultCleanStaleUploadsPeriodicArgs(), System: true},
		{Name: "Clean Pending Files", Kind: periodicJobKindCleanPendingFile, CronExpression: "0 * * * *", Args: repositories.CleanPendingFilesPeriodicArgs{}, System: true},
		{Name: "Refresh Folder Sizes", Kind: periodicJobKindRefreshFolderSize, CronExpression: "0 * * * *", Args: repositories.RefreshFolderSizesPeriodicArgs{}, System: true},
	}
}

func defaultCleanOldEventsPeriodicArgs() repositories.CleanOldEventsPeriodicArgs {
	return repositories.CleanOldEventsPeriodicArgs{Retention: defaultOldEventsRetention}
}

func defaultCleanStaleUploadsPeriodicArgs() repositories.CleanStaleUploadsPeriodicArgs {
	return repositories.CleanStaleUploadsPeriodicArgs{Retention: defaultStaleUploadRetention}
}

func normalizePeriodicJobArgs(kind string, args repositories.PeriodicJobArgs) repositories.PeriodicJobArgs {
	switch kind {
	case periodicJobKindCleanOldEvents:
		return normalizeCleanOldEventsPeriodicArgs(args)
	case periodicJobKindCleanStaleUpload:
		return normalizeCleanStaleUploadsPeriodicArgs(args)
	case periodicJobKindRefreshFolderSize:
		return repositories.RefreshFolderSizesPeriodicArgs{}
	default:
		return args
	}
}

func normalizeCleanOldEventsPeriodicArgs(args repositories.PeriodicJobArgs) repositories.CleanOldEventsPeriodicArgs {
	defaultArgs := defaultCleanOldEventsPeriodicArgs()
	switch v := args.(type) {
	case repositories.CleanOldEventsPeriodicArgs:
		if normalized, ok := normalizeRetentionString(v.Retention); ok {
			return repositories.CleanOldEventsPeriodicArgs{Retention: normalized}
		}
	case *repositories.CleanOldEventsPeriodicArgs:
		if v != nil {
			if normalized, ok := normalizeRetentionString(v.Retention); ok {
				return repositories.CleanOldEventsPeriodicArgs{Retention: normalized}
			}
		}
	}
	return defaultArgs
}

func normalizeCleanStaleUploadsPeriodicArgs(args repositories.PeriodicJobArgs) repositories.CleanStaleUploadsPeriodicArgs {
	defaultArgs := defaultCleanStaleUploadsPeriodicArgs()
	switch v := args.(type) {
	case repositories.CleanStaleUploadsPeriodicArgs:
		if normalized, ok := normalizeRetentionString(v.Retention); ok {
			return repositories.CleanStaleUploadsPeriodicArgs{Retention: normalized}
		}
	case *repositories.CleanStaleUploadsPeriodicArgs:
		if v != nil {
			if normalized, ok := normalizeRetentionString(v.Retention); ok {
				return repositories.CleanStaleUploadsPeriodicArgs{Retention: normalized}
			}
		}
	}
	return defaultArgs
}

func normalizeRetentionString(raw string) (string, bool) {
	d, err := internalduration.ParseDuration(strings.TrimSpace(raw))
	if err != nil || d <= 0 {
		return "", false
	}
	formatted := internalduration.Duration(d)
	return formatted.String(), true
}

func (a *apiService) ensurePeriodicPresetArgs(ctx context.Context, row *periodicJobRow) (*periodicJobRow, error) {
	normalizedArgs := normalizePeriodicJobArgs(row.Kind, row.Args)
	if !periodicJobArgsNeedUpdate(row.Kind, row.Args, normalizedArgs) {
		return nil, nil
	}
	updatedAt := time.Now().UTC()
	if err := a.repo.PeriodicJobs.Update(ctx, uuid.MustParse(row.ID), row.UserID, repositories.PeriodicJob{
		Kind:           row.Kind,
		Name:           row.Name,
		CronExpression: row.CronExpression,
		Enabled:        row.Enabled,
		Args:           normalizedArgs,
		UpdatedAt:      updatedAt,
	}); err != nil {
		return nil, &apiError{err: err}
	}
	row.Args = normalizedArgs
	row.UpdatedAt = updatedAt
	return row, nil
}

func periodicJobArgsNeedUpdate(kind string, current, normalized repositories.PeriodicJobArgs) bool {
	switch kind {
	case periodicJobKindCleanOldEvents:
		normalizedArgs, ok := normalized.(repositories.CleanOldEventsPeriodicArgs)
		if !ok {
			return false
		}
		switch v := current.(type) {
		case repositories.CleanOldEventsPeriodicArgs:
			return v.Retention != normalizedArgs.Retention
		case *repositories.CleanOldEventsPeriodicArgs:
			if v == nil {
				return true
			}
			return v.Retention != normalizedArgs.Retention
		default:
			return true
		}
	case periodicJobKindCleanStaleUpload:
		normalizedArgs, ok := normalized.(repositories.CleanStaleUploadsPeriodicArgs)
		if !ok {
			return false
		}
		switch v := current.(type) {
		case repositories.CleanStaleUploadsPeriodicArgs:
			return v.Retention != normalizedArgs.Retention
		case *repositories.CleanStaleUploadsPeriodicArgs:
			if v == nil {
				return true
			}
			return v.Retention != normalizedArgs.Retention
		default:
			return true
		}
	default:
		return false
	}
}

func (a *apiService) insertPeriodicPreset(ctx context.Context, userID int64, preset periodicJobPreset) (*periodicJobRow, error) {
	id := uuid.NewString()
	jobModel := &repositories.PeriodicJob{
		ID:             uuid.MustParse(id),
		UserID:         userID,
		Name:           preset.Name,
		Kind:           preset.Kind,
		Args:           preset.Args,
		CronExpression: preset.CronExpression,
		Enabled:        true,
		System:         preset.System,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}
	err := a.repo.PeriodicJobs.Create(ctx, jobModel)
	if err != nil {
		return nil, &apiError{err: err}
	}
	return a.getPeriodicJobRow(ctx, id, userID)
}

func (a *apiService) getPeriodicJobByName(ctx context.Context, userID int64, name string) (*periodicJobRow, error) {
	item, err := a.repo.PeriodicJobs.GetByNameAndUserID(ctx, userID, name)
	if err != nil {
		if errors.Is(err, repositories.ErrNotFound) {
			return nil, &apiError{err: periodicJobNotFound(name, err)}
		}
		return nil, &apiError{err: err}
	}
	return fromPeriodicJobModel(*item), nil
}

func (a *apiService) getPeriodicJobRow(ctx context.Context, id string, userID int64) (*periodicJobRow, error) {
	item, err := a.repo.PeriodicJobs.GetByIDAndUserID(ctx, uuid.MustParse(id), userID)
	if err != nil {
		if errors.Is(err, repositories.ErrNotFound) {
			return nil, &apiError{err: periodicJobNotFound(id, err)}
		}
		return nil, &apiError{err: err}
	}
	return fromPeriodicJobModel(*item), nil
}

func fromPeriodicJobModel(item repositories.PeriodicJob) *periodicJobRow {
	return &periodicJobRow{
		ID:             item.ID.String(),
		UserID:         item.UserID,
		Name:           item.Name,
		Kind:           item.Kind,
		Args:           normalizePeriodicJobArgs(item.Kind, item.Args),
		CronExpression: item.CronExpression,
		Enabled:        item.Enabled,
		System:         item.System,
		CreatedAt:      item.CreatedAt,
		UpdatedAt:      item.UpdatedAt,
	}
}

func toAPIPeriodicJobSummary(row *periodicJobRow) (*api.PeriodicJobSummary, error) {
	out := &api.PeriodicJobSummary{
		ID:             api.UUID(uuid.MustParse(row.ID)),
		Name:           row.Name,
		Kind:           api.PeriodicJobKind(row.Kind),
		Enabled:        row.Enabled,
		CronExpression: row.CronExpression,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
	}
	return out, nil
}

func toAPIPeriodicJobDetail(row *periodicJobRow) (*api.PeriodicJobDetail, error) {
	out := &api.PeriodicJobDetail{
		ID:             api.UUID(uuid.MustParse(row.ID)),
		Name:           row.Name,
		Kind:           api.PeriodicJobKind(row.Kind),
		Enabled:        row.Enabled,
		CronExpression: row.CronExpression,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
	}
	if args := toAPIPeriodicJobDetailArgs(row.Args); len(args) > 0 {
		out.Args = api.NewOptPeriodicJobDetailArgs(args)
	}
	return out, nil
}

func validatePeriodicCron(expr string) error {
	if expr == "" {
		return fmt.Errorf("cronExpression is required")
	}
	if _, err := cron.ParseStandard(expr); err != nil {
		return fmt.Errorf("invalid cron expression: %w", err)
	}
	return nil
}

func toAPIPeriodicJobDetailArgs(v repositories.PeriodicJobArgs) api.PeriodicJobDetailArgs {
	b, err := json.Marshal(v)
	if err != nil || len(b) == 0 || string(b) == "{}" {
		return nil
	}
	raw := map[string]json.RawMessage{}
	if err := json.Unmarshal(b, &raw); err != nil || len(raw) == 0 {
		return nil
	}
	out := make(api.PeriodicJobDetailArgs, len(raw))
	for k, rv := range raw {
		out[k] = jx.Raw(rv)
	}
	return out
}
