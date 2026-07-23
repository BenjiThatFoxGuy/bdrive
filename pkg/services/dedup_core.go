package services

import (
	"context"

	"github.com/gotd/td/telegram"
	"gorm.io/gorm"

	"github.com/tgdrive/teldrive/internal/api"
	"github.com/tgdrive/teldrive/internal/cache"
	"github.com/tgdrive/teldrive/internal/config"
	"github.com/tgdrive/teldrive/internal/database"
	"github.com/tgdrive/teldrive/internal/tgc"
	"github.com/tgdrive/teldrive/pkg/models"
)

// dedupLockRetries is how many times a single file's link/hash write is retried
// when it hits a transient pgroonga index lock before the file is skipped.
const dedupLockRetries = 5

// Dedup phase identifiers. These mirror the DedupPhase enum in the API spec
// (see api.DedupPhase*) but are kept as plain strings so the core stays
// decoupled from how each caller renders them.
const (
	dedupPhaseLoading     = string(api.DedupPhaseLoading)
	dedupPhaseBackfilling = string(api.DedupPhaseBackfilling)
	dedupPhaseGrouping    = string(api.DedupPhaseGrouping)
	dedupPhaseLinking     = string(api.DedupPhaseLinking)
	dedupPhaseDone        = string(api.DedupPhaseDone)
)

// DedupDeps bundles the dependencies the dedup core needs. The API service and
// the standalone CLI construct these differently (Redis vs in-memory cache, and
// so on), but the core dedup logic is identical.
type DedupDeps struct {
	DB             *gorm.DB
	Cache          cache.Cacher
	TG             *config.TGConfig
	ChannelManager *tgc.ChannelManager
	BotSelector    tgc.BotSelector
}

// DedupHooks are optional callbacks invoked as a dedup run progresses. Any nil
// field is skipped, so each caller implements only what it needs: the async API
// job reports progress into the tracked job, while the CLI prints it.
type DedupHooks struct {
	// OnPhase fires when the run enters a new phase; total is the number of
	// items that phase will process, or 0 when not countable.
	OnPhase func(phase string, total int64)
	// OnProgress fires after each item within a phase; current counts items done
	// so far and total repeats the phase total.
	OnProgress func(phase string, current, total int64)
	// OnGroup fires once per duplicate group, with its canonical file.
	OnGroup func(hash string, count int, canonical models.File)
	// OnLinked fires after a duplicate is linked to its canonical copy.
	OnLinked func(dup, canonical models.File)
	// OnBackfilled fires after a file's hash is computed (and persisted unless
	// dry-run).
	OnBackfilled func(file models.File)
	// OnSkipped fires when a file cannot be processed and is skipped.
	OnSkipped func(file models.File, err error)
}

func (h DedupHooks) phase(phase string, total int64) {
	if h.OnPhase != nil {
		h.OnPhase(phase, total)
	}
}

func (h DedupHooks) progress(phase string, current, total int64) {
	if h.OnProgress != nil {
		h.OnProgress(phase, current, total)
	}
}

func (h DedupHooks) group(hash string, count int, canonical models.File) {
	if h.OnGroup != nil {
		h.OnGroup(hash, count, canonical)
	}
}

func (h DedupHooks) linked(dup, canonical models.File) {
	if h.OnLinked != nil {
		h.OnLinked(dup, canonical)
	}
}

func (h DedupHooks) backfilled(file models.File) {
	if h.OnBackfilled != nil {
		h.OnBackfilled(file)
	}
}

func (h DedupHooks) skipped(file models.File, err error) {
	if h.OnSkipped != nil {
		h.OnSkipped(file, err)
	}
}

// RunDedupForUser groups userID's active, non-encrypted files by content hash
// and links duplicates to a canonical copy, accumulating counters into stats
// and reporting progress through hooks. It is the single implementation shared
// by the async API job (pkg/services/dedup.go) and the `teldrive deduplicate`
// CLI command (cmd/deduplicate.go).
func RunDedupForUser(ctx context.Context, deps DedupDeps, userID int64, dryRun, backfill bool, stats *api.DedupStats, hooks DedupHooks) error {
	hooks.phase(dedupPhaseLoading, 0)

	var files []models.File
	if err := deps.DB.WithContext(ctx).Where(
		"user_id = ? AND hash IS NOT NULL AND hash != '' AND encrypted = false AND status = 'active' AND type = 'file'",
		userID,
	).Find(&files).Error; err != nil {
		return err
	}

	if backfill {
		backfilled, err := backfillUserHashes(ctx, deps, userID, dryRun, stats, hooks)
		if err != nil {
			return err
		}
		files = append(files, backfilled...)
	}

	// Group by hash. Grouping is in-memory and fast, so it isn't item-countable.
	hooks.phase(dedupPhaseGrouping, 0)
	hashGroups := make(map[string][]models.File)
	for _, f := range files {
		if f.Hash != nil && *f.Hash != "" {
			hashGroups[*f.Hash] = append(hashGroups[*f.Hash], f)
		}
	}

	// Count the links up front so the linking phase has a denominator.
	var totalLinks int64
	for _, group := range hashGroups {
		if len(group) > 1 {
			totalLinks += int64(len(group) - 1)
		}
	}

	hooks.phase(dedupPhaseLinking, totalLinks)
	var linked int64
	advance := func() {
		linked++
		hooks.progress(dedupPhaseLinking, linked, totalLinks)
	}

	for hash, group := range hashGroups {
		if len(group) <= 1 {
			continue
		}
		stats.DuplicateGroups++
		canonical := group[0]
		hooks.group(hash, len(group), canonical)

		for i := 1; i < len(group); i++ {
			dup := group[i]
			if !dryRun {
				err := database.RetryTransientLock(ctx, dedupLockRetries, func() error {
					return deps.DB.WithContext(ctx).Model(&models.File{}).
						Where("id = ?", dup.ID).
						Updates(map[string]any{
							"referenced_file_id": canonical.ID,
							"parts":              canonical.Parts,
							"channel_id":         canonical.ChannelId,
						}).Error
				})
				if err != nil {
					// A pgroonga lock that survives every retry shouldn't discard
					// the progress already made: skip this file and keep going so
					// the run still completes and reports it. Any other error is
					// fatal to the run.
					if database.IsTransientLockErr(err) {
						stats.SkippedFiles++
						hooks.skipped(dup, err)
						advance()
						continue
					}
					return err
				}
			}
			stats.TotalFilesLinked++
			hooks.linked(dup, canonical)
			advance()
		}
	}

	hooks.phase(dedupPhaseDone, 0)
	return nil
}

// backfillUserHashes computes and (unless dryRun) persists a content hash for
// userID's files that lack one, so they can participate in grouping. Content is
// re-read from Telegram. Returns the files it successfully hashed (Hash set
// in-memory, even in dry-run) so the caller can fold them into grouping.
func backfillUserHashes(ctx context.Context, deps DedupDeps, userID int64, dryRun bool, stats *api.DedupStats, hooks DedupHooks) ([]models.File, error) {
	var files []models.File
	if err := deps.DB.WithContext(ctx).Where(
		"user_id = ? AND (hash IS NULL OR hash = '') AND encrypted = false AND status = 'active' AND type = 'file'",
		userID,
	).Find(&files).Error; err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, nil
	}

	total := int64(len(files))
	hooks.phase(dedupPhaseBackfilling, total)

	var session models.Session
	fallbackSession := ""
	if err := deps.DB.Where("user_id = ?", userID).First(&session).Error; err == nil {
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
		c, t, b, err := ResolveUserClient(ctx, deps.DB, deps.Cache, deps.TG, deps.ChannelManager, deps.BotSelector, userID, fallbackSession)
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
			newHash, hashErr = ComputeFileContentHash(ctx, nil, deps.Cache, deps.TG, "", &file)
		} else if err := resolveClient(); err != nil {
			hashErr = err
		} else {
			hashErr = tgc.RunWithAuth(ctx, client, token, func(ctx context.Context) error {
				h, err := ComputeFileContentHash(ctx, client, deps.Cache, deps.TG, botID, &file)
				if err != nil {
					return err
				}
				newHash = h
				return nil
			})
		}

		if hashErr != nil {
			stats.SkippedFiles++
			hooks.skipped(file, hashErr)
			hooks.progress(dedupPhaseBackfilling, int64(i+1), total)
			continue
		}

		if !dryRun {
			if err := database.RetryTransientLock(ctx, dedupLockRetries, func() error {
				return deps.DB.WithContext(ctx).Model(&models.File{}).Where("id = ?", file.ID).Update("hash", newHash).Error
			}); err != nil {
				stats.SkippedFiles++
				hooks.skipped(file, err)
				hooks.progress(dedupPhaseBackfilling, int64(i+1), total)
				continue
			}
		}

		file.Hash = &newHash
		stats.HashesBackfilled++
		backfilled = append(backfilled, file)
		hooks.backfilled(file)
		hooks.progress(dedupPhaseBackfilling, int64(i+1), total)
	}

	return backfilled, nil
}
