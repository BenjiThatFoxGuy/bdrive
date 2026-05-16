package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
)

// CronScheduler runs due periodic_jobs directly. It is not a task queue.
type CronScheduler struct {
	pool     *pgxpool.Pool
	store    *Store
	handlers map[string]Handler
	cfg      CronSchedulerConfig
	log      *zap.Logger
	parser   cron.Parser

	running atomic.Bool
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

type CronSchedulerConfig struct {
	PollInterval time.Duration
	LockID       int64
}

func DefaultCronSchedulerConfig() CronSchedulerConfig {
	return CronSchedulerConfig{
		PollInterval: 30 * time.Second,
		LockID:       2123216947,
	}
}

func NewCronScheduler(pool *pgxpool.Pool, store *Store, handlers []HandlerDef, cfg CronSchedulerConfig, log *zap.Logger) *CronScheduler {
	ctx, cancel := context.WithCancel(context.Background())
	handlerMap := make(map[string]Handler, len(handlers))
	for _, h := range handlers {
		handlerMap[h.Kind] = h.Handler
	}
	return &CronScheduler{
		pool:     pool,
		store:    store,
		handlers: handlerMap,
		cfg:      cfg,
		log:      log.Named("worker.cron"),
		parser:   cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow),
		ctx:      ctx,
		cancel:   cancel,
	}
}

func (s *CronScheduler) Start() {
	if s.running.Swap(true) {
		return
	}
	s.log.Info("starting cron background jobs", zap.Duration("poll_interval", s.cfg.PollInterval))
	s.wg.Add(1)
	go s.scheduleLoop()
}

func (s *CronScheduler) Stop() {
	if !s.running.Swap(false) {
		return
	}
	s.cancel()
	s.wg.Wait()
	s.log.Info("cron background jobs stopped")
}

func (s *CronScheduler) scheduleLoop() {
	defer s.wg.Done()
	ticker := time.NewTicker(s.cfg.PollInterval)
	defer ticker.Stop()

	s.tryRunDueJobs()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.tryRunDueJobs()
		}
	}
}

func (s *CronScheduler) tryRunDueJobs() {
	acquired := false
	err := s.pool.QueryRow(s.ctx, "SELECT pg_try_advisory_lock($1)", s.cfg.LockID).Scan(&acquired)
	if err != nil || !acquired {
		return
	}
	defer func() {
		_, _ = s.pool.Exec(s.ctx, "SELECT pg_advisory_unlock($1)", s.cfg.LockID)
	}()

	jobs, err := s.store.ListDue(s.ctx, 50)
	if err != nil {
		s.log.Error("query due background jobs", zap.Error(err))
		return
	}
	for _, job := range jobs {
		s.run(job)
	}
}

func (s *CronScheduler) run(cronJob *CronJob) {
	handler := s.handlers[cronJob.Kind]
	if handler == nil {
		s.log.Warn("no handler for background job", zap.String("kind", cronJob.Kind), zap.String("id", cronJob.ID))
		nextRunAt := s.nextRun(cronJob)
		_ = s.store.MarkFailed(s.ctx, cronJob.ID, nextRunAt, fmt.Sprintf("no handler registered for kind: %s", cronJob.Kind))
		return
	}

	startedAt := time.Now().UTC()
	if err := s.store.MarkRunning(s.ctx, cronJob.ID, startedAt); err != nil {
		s.log.Error("mark background job running", zap.String("id", cronJob.ID), zap.Error(err))
		return
	}

	args := s.buildJobArgs(cronJob)
	if args == nil {
		nextRunAt := s.nextRun(cronJob)
		_ = s.store.MarkFailed(s.ctx, cronJob.ID, nextRunAt, "invalid background job args")
		return
	}

	job := &Job{ID: cronJob.ID, UserID: cronJob.UserID, Kind: cronJob.Kind, Args: args, State: JobStateRunning}
	if err := handler(s.ctx, job); err != nil {
		nextRunAt := s.nextRun(cronJob)
		if storeErr := s.store.MarkFailed(s.ctx, cronJob.ID, nextRunAt, err.Error()); storeErr != nil {
			s.log.Error("mark background job failed", zap.String("id", cronJob.ID), zap.Error(storeErr))
		}
		s.log.Error("background job failed", zap.String("id", cronJob.ID), zap.String("kind", cronJob.Kind), zap.Error(err))
		return
	}

	nextRunAt := s.nextRun(cronJob)
	if err := s.store.MarkSucceeded(s.ctx, cronJob.ID, nextRunAt); err != nil {
		s.log.Error("mark background job succeeded", zap.String("id", cronJob.ID), zap.Error(err))
	}
}

func (s *CronScheduler) nextRun(cronJob *CronJob) time.Time {
	schedule, err := s.parser.Parse(cronJob.CronExpression)
	if err != nil {
		return time.Now().UTC().Add(s.cfg.PollInterval)
	}
	return schedule.Next(time.Now().UTC())
}

func (s *CronScheduler) buildJobArgs(cronJob *CronJob) []byte {
	switch cronJob.Kind {
	case "clean.old_events", "clean.stale_uploads", "clean.pending_files", "refresh.folder_sizes":
		var raw map[string]any
		if err := json.Unmarshal(cronJob.Args, &raw); err != nil {
			raw = make(map[string]any)
		}
		raw["userId"] = cronJob.UserID
		b, _ := json.Marshal(raw)
		return b
	default:
		return nil
	}
}
