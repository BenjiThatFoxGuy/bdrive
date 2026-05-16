package worker

import (
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

// Worker is the top-level cron background job runner.
type Worker struct {
	Scheduler *CronScheduler
	Store     *Store
}

type Config struct {
	CronPollEvery time.Duration
	CronLockID    int64
}

func DefaultConfig() Config {
	return Config{
		CronPollEvery: 30 * time.Second,
		CronLockID:    2123216947,
	}
}

func New(pool *pgxpool.Pool, handlers []HandlerDef, cfg Config, log *zap.Logger) *Worker {
	store := NewStore(pool)

	schedCfg := DefaultCronSchedulerConfig()
	schedCfg.PollInterval = cfg.CronPollEvery
	schedCfg.LockID = cfg.CronLockID

	scheduler := NewCronScheduler(pool, store, handlers, schedCfg, log)

	return &Worker{
		Scheduler: scheduler,
		Store:     store,
	}
}

func (w *Worker) Start() {
	w.Scheduler.Start()
}

func (w *Worker) Stop() {
	w.Scheduler.Stop()
}
