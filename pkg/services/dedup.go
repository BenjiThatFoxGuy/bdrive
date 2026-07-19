package services

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tgdrive/teldrive/internal/api"
	"github.com/tgdrive/teldrive/internal/auth"
	"github.com/tgdrive/teldrive/internal/logging"
	"github.com/tgdrive/teldrive/pkg/mapper"
	"github.com/tgdrive/teldrive/pkg/models"
	"go.uber.org/zap"
)

// maxDedupJobsPerUser bounds the in-memory job history kept per user so a
// long-lived server doesn't accumulate records without limit.
const maxDedupJobsPerUser = 20

// dedupJobRecord is a single tracked job together with its owner.
type dedupJobRecord struct {
	userID int64
	job    api.DedupJob
}

// dedupManager is an in-memory store of deduplication jobs. Jobs are ephemeral
// and scoped to the process; this mirrors the "list recent jobs" contract and
// keeps the heavy, long-running dedup work off the request goroutine without
// requiring extra persistence. Access is guarded by mu.
type dedupManager struct {
	mu   sync.Mutex
	jobs map[string]*dedupJobRecord
}

func newDedupManager() *dedupManager {
	return &dedupManager{jobs: make(map[string]*dedupJobRecord)}
}

func (m *dedupManager) create(userID int64, opts api.DedupJobOptions) api.DedupJob {
	m.mu.Lock()
	defer m.mu.Unlock()

	job := api.DedupJob{
		ID:        uuid.NewString(),
		Status:    api.DedupJobStatusPending,
		Options:   opts,
		Stats:     api.DedupStats{},
		StartedAt: time.Now().UTC(),
	}
	m.jobs[job.ID] = &dedupJobRecord{userID: userID, job: job}
	m.pruneLocked(userID)
	return job
}

// update applies fn to the stored job under the lock.
func (m *dedupManager) update(id string, fn func(*api.DedupJob)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if rec, ok := m.jobs[id]; ok {
		fn(&rec.job)
	}
}

func (m *dedupManager) get(userID int64, id string) (api.DedupJob, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.jobs[id]
	if !ok || rec.userID != userID {
		return api.DedupJob{}, false
	}
	return rec.job, true
}

func (m *dedupManager) list(userID int64) []api.DedupJob {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]api.DedupJob, 0)
	for _, rec := range m.jobs {
		if rec.userID == userID {
			out = append(out, rec.job)
		}
	}
	return out
}

// pruneLocked drops the oldest jobs for userID beyond maxDedupJobsPerUser.
// Caller must hold m.mu.
func (m *dedupManager) pruneLocked(userID int64) {
	var owned []*dedupJobRecord
	for _, rec := range m.jobs {
		if rec.userID == userID {
			owned = append(owned, rec)
		}
	}
	if len(owned) <= maxDedupJobsPerUser {
		return
	}
	// Remove the oldest (by StartedAt) until within the limit.
	for len(owned) > maxDedupJobsPerUser {
		oldestIdx := 0
		for i, rec := range owned {
			if rec.job.StartedAt.Before(owned[oldestIdx].job.StartedAt) {
				oldestIdx = i
			}
		}
		delete(m.jobs, owned[oldestIdx].job.ID)
		owned = append(owned[:oldestIdx], owned[oldestIdx+1:]...)
	}
}

// DedupStartJob starts an asynchronous deduplication run for the calling user
// and returns the created job. Targeting another user or all users is admin
// only and not yet enabled, so those options are rejected.
func (a *apiService) DedupStartJob(ctx context.Context, req *api.DedupJobOptions) (*api.DedupJob, error) {
	userID := auth.GetUser(ctx)

	if req.User.Or("") != "" || req.AllUsers.Or(false) {
		return nil, &apiError{
			err:  errors.New("targeting another user or all users is admin only and not enabled"),
			code: http.StatusForbidden,
		}
	}

	job := a.dedup.create(userID, *req)

	// Detach from the request context so the run survives the response, but
	// keep context values (auth, logging) intact.
	runCtx := context.WithoutCancel(ctx)
	go a.runDedupJob(runCtx, job.ID, userID, *req)

	return &job, nil
}

// DedupListJobs returns the caller's recent deduplication jobs.
func (a *apiService) DedupListJobs(ctx context.Context) ([]api.DedupJob, error) {
	return a.dedup.list(auth.GetUser(ctx)), nil
}

// DedupGetJob returns one of the caller's deduplication jobs by ID.
func (a *apiService) DedupGetJob(ctx context.Context, params api.DedupGetJobParams) (*api.DedupJob, error) {
	job, ok := a.dedup.get(auth.GetUser(ctx), params.ID)
	if !ok {
		return nil, &apiError{err: errors.New("dedup job not found"), code: http.StatusNotFound}
	}
	return &job, nil
}

// FilesDuplicates returns the caller's other active, non-encrypted files that
// share the same content hash as the given file.
func (a *apiService) FilesDuplicates(ctx context.Context, params api.FilesDuplicatesParams) (*api.FileDuplicates, error) {
	userID := auth.GetUser(ctx)

	var source models.File
	if err := a.db.WithContext(ctx).Where("id = ? AND user_id = ?", params.ID, userID).First(&source).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, &apiError{err: errors.New("file not found"), code: http.StatusNotFound}
		}
		return nil, &apiError{err: err, code: http.StatusInternalServerError}
	}

	if source.Hash == nil || *source.Hash == "" {
		return &api.FileDuplicates{Items: []api.File{}}, nil
	}

	var dups []models.File
	if err := a.db.WithContext(ctx).Where(
		"user_id = ? AND hash = ? AND id != ? AND encrypted = false AND status = 'active'",
		userID, *source.Hash, params.ID,
	).Find(&dups).Error; err != nil {
		return nil, &apiError{err: err, code: http.StatusInternalServerError}
	}

	items := make([]api.File, 0, len(dups))
	for i := range dups {
		items = append(items, *mapper.ToFileOut(dups[i]))
	}
	return &api.FileDuplicates{Items: items}, nil
}

// DedupStats returns a read-only snapshot of the caller's current dedup state
// without starting a run.
func (a *apiService) DedupStats(ctx context.Context) (*api.DedupStats, error) {
	userID := auth.GetUser(ctx)
	stats, err := a.currentDedupStats(userID)
	if err != nil {
		return nil, &apiError{err: err, code: http.StatusInternalServerError}
	}
	return stats, nil
}

// currentDedupStats computes the caller's dedup state directly from the
// database: how many duplicate content groups exist and how many files are
// already linked to a canonical copy.
func (a *apiService) currentDedupStats(userID int64) (*api.DedupStats, error) {
	var duplicateGroups int64
	if err := a.db.
		Raw(`SELECT COUNT(*) FROM (
			SELECT hash FROM teldrive.files
			WHERE user_id = ? AND hash IS NOT NULL AND hash != ''
			  AND encrypted = false AND status = 'active' AND type = 'file'
			GROUP BY hash HAVING COUNT(*) > 1
		) g`, userID).Scan(&duplicateGroups).Error; err != nil {
		return nil, err
	}

	var totalFilesLinked int64
	if err := a.db.Model(&models.File{}).
		Where("user_id = ? AND referenced_file_id IS NOT NULL AND status = 'active'", userID).
		Count(&totalFilesLinked).Error; err != nil {
		return nil, err
	}

	return &api.DedupStats{
		ProcessedUsers:   1,
		DuplicateGroups:  duplicateGroups,
		TotalFilesLinked: totalFilesLinked,
		HashesBackfilled: 0,
		SkippedFiles:     0,
	}, nil
}

// runDedupJob executes a deduplication run for a single user, updating the
// tracked job as it progresses. It runs the shared dedup core (see
// runDedupForUser) with hooks that publish live progress and a running stats
// snapshot into the tracked job, so pollers see the run advance rather than a
// single jump at the end.
func (a *apiService) runDedupJob(ctx context.Context, jobID string, userID int64, opts api.DedupJobOptions) {
	logger := logging.FromContext(ctx)

	a.dedup.update(jobID, func(j *api.DedupJob) { j.Status = api.DedupJobStatusRunning })

	dryRun := opts.DryRun.Or(false)
	backfill := opts.Backfill.Or(false)

	stats := api.DedupStats{ProcessedUsers: 1}

	deps := DedupDeps{
		DB:             a.db,
		Cache:          a.cache,
		TG:             &a.cnf.TG,
		ChannelManager: a.channelManager,
		BotSelector:    a.botSelector,
	}

	publish := func(phase string, current, total int64) {
		a.dedup.update(jobID, func(j *api.DedupJob) {
			j.Stats = stats
			j.Progress = api.NewOptDedupProgress(api.DedupProgress{
				Phase:   api.DedupPhase(phase),
				Current: current,
				Total:   total,
			})
		})
	}
	hooks := DedupHooks{
		OnPhase:    func(phase string, total int64) { publish(phase, 0, total) },
		OnProgress: func(phase string, current, total int64) { publish(phase, current, total) },
	}

	if err := RunDedupForUser(ctx, deps, userID, dryRun, backfill, &stats, hooks); err != nil {
		logger.Error("dedup.job.failed", zap.String("job", jobID), zap.Error(err))
		a.dedup.update(jobID, func(j *api.DedupJob) {
			j.Status = api.DedupJobStatusFailed
			j.Error = api.NewOptString(err.Error())
			j.Stats = stats
			j.Progress = api.OptDedupProgress{}
			j.FinishedAt = api.NewOptDateTime(time.Now().UTC())
		})
		return
	}

	a.dedup.update(jobID, func(j *api.DedupJob) {
		j.Status = api.DedupJobStatusCompleted
		j.Stats = stats
		j.Progress = api.OptDedupProgress{}
		j.FinishedAt = api.NewOptDateTime(time.Now().UTC())
	})
}
