package services

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gotd/td/telegram"
	"gorm.io/gorm"

	"github.com/tgdrive/teldrive/internal/api"
	"github.com/tgdrive/teldrive/internal/auth"
	"github.com/tgdrive/teldrive/internal/logging"
	"github.com/tgdrive/teldrive/internal/tgc"
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
// tracked job as it progresses. It mirrors the `teldrive deduplicate` command's
// grouping (and optional hash backfill) without the interactive/console parts.
func (a *apiService) runDedupJob(ctx context.Context, jobID string, userID int64, opts api.DedupJobOptions) {
	logger := logging.FromContext(ctx)

	a.dedup.update(jobID, func(j *api.DedupJob) { j.Status = api.DedupJobStatusRunning })

	dryRun := opts.DryRun.Or(false)
	backfill := opts.Backfill.Or(false)

	stats := api.DedupStats{ProcessedUsers: 1}

	if err := a.dedupUser(ctx, userID, dryRun, backfill, &stats); err != nil {
		logger.Error("dedup.job.failed", zap.String("job", jobID), zap.Error(err))
		a.dedup.update(jobID, func(j *api.DedupJob) {
			j.Status = api.DedupJobStatusFailed
			j.Error = api.NewOptString(err.Error())
			j.Stats = stats
			j.FinishedAt = api.NewOptDateTime(time.Now().UTC())
		})
		return
	}

	a.dedup.update(jobID, func(j *api.DedupJob) {
		j.Status = api.DedupJobStatusCompleted
		j.Stats = stats
		j.FinishedAt = api.NewOptDateTime(time.Now().UTC())
	})
}

// dedupUser groups the user's active, non-encrypted files by content hash and
// links duplicates to a canonical copy, accumulating counters into stats.
func (a *apiService) dedupUser(ctx context.Context, userID int64, dryRun, backfill bool, stats *api.DedupStats) error {
	var files []models.File
	if err := a.db.WithContext(ctx).Where(
		"user_id = ? AND hash IS NOT NULL AND hash != '' AND encrypted = false AND status = 'active' AND type = 'file'",
		userID,
	).Find(&files).Error; err != nil {
		return err
	}

	if backfill {
		backfilled, err := a.backfillUserHashes(ctx, userID, dryRun, stats)
		if err != nil {
			return err
		}
		files = append(files, backfilled...)
	}

	// Group by hash and link every non-canonical file in a group of duplicates.
	hashGroups := make(map[string][]models.File)
	for _, f := range files {
		if f.Hash != nil && *f.Hash != "" {
			hashGroups[*f.Hash] = append(hashGroups[*f.Hash], f)
		}
	}

	for _, group := range hashGroups {
		if len(group) <= 1 {
			continue
		}
		stats.DuplicateGroups++
		canonical := group[0]
		for i := 1; i < len(group); i++ {
			dup := group[i]
			if !dryRun {
				if err := a.db.WithContext(ctx).Model(&models.File{}).
					Where("id = ?", dup.ID).
					Updates(map[string]any{
						"referenced_file_id": canonical.ID,
						"parts":              canonical.Parts,
						"channel_id":         canonical.ChannelId,
					}).Error; err != nil {
					return err
				}
			}
			stats.TotalFilesLinked++
		}
	}

	return nil
}

// backfillUserHashes computes and (unless dryRun) persists a content hash for
// the user's files that lack one, so they can participate in grouping. Content
// is re-read from Telegram, mirroring the deduplicate command's --backfill.
func (a *apiService) backfillUserHashes(ctx context.Context, userID int64, dryRun bool, stats *api.DedupStats) ([]models.File, error) {
	var files []models.File
	if err := a.db.WithContext(ctx).Where(
		"user_id = ? AND (hash IS NULL OR hash = '') AND encrypted = false AND status = 'active' AND type = 'file'",
		userID,
	).Find(&files).Error; err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, nil
	}

	var session models.Session
	fallbackSession := ""
	if err := a.db.Where("user_id = ?", userID).First(&session).Error; err == nil {
		fallbackSession = session.Session
	}

	var (
		client *telegram.Client
		token  string
		botID  string
	)
	resolveClient := func() error {
		if client != nil {
			return nil
		}
		c, t, b, err := ResolveUserClient(ctx, a.db, a.cache, &a.cnf.TG, a.channelManager, a.botSelector, userID, fallbackSession)
		if err != nil {
			return err
		}
		client, token, botID = c, t, b
		return nil
	}

	backfilled := make([]models.File, 0, len(files))
	for i := range files {
		file := files[i]

		var (
			newHash string
			hashErr error
		)
		if file.Size == nil || *file.Size == 0 {
			newHash, hashErr = ComputeFileContentHash(ctx, nil, a.cache, &a.cnf.TG, "", &file)
		} else if err := resolveClient(); err != nil {
			hashErr = err
		} else {
			hashErr = tgc.RunWithAuth(ctx, client, token, func(ctx context.Context) error {
				h, err := ComputeFileContentHash(ctx, client, a.cache, &a.cnf.TG, botID, &file)
				if err != nil {
					return err
				}
				newHash = h
				return nil
			})
		}

		if hashErr != nil {
			stats.SkippedFiles++
			continue
		}

		if !dryRun {
			if err := a.db.WithContext(ctx).Model(&models.File{}).Where("id = ?", file.ID).Update("hash", newHash).Error; err != nil {
				stats.SkippedFiles++
				continue
			}
		}

		file.Hash = &newHash
		stats.HashesBackfilled++
		backfilled = append(backfilled, file)
	}

	return backfilled, nil
}
