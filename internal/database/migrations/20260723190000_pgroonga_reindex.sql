-- +goose Up
-- Defensive rebuild of the pgroonga indexes on teldrive.files.name. Repeated
-- "grn_io_lock failed" contention (see internal/database.IsTransientLockErr)
-- from bursts of writes during dedup runs stresses pgroonga's index storage,
-- which lives outside Postgres's normal heap/WAL. A REINDEX guarantees both
-- indexes are rebuilt cleanly from the current table contents.
-- Skipped when pgroonga is not installed (optional-search fallback mode).
-- +goose StatementBegin
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pgroonga')
     AND to_regclass('teldrive.idx_files_name_search') IS NOT NULL THEN
    REINDEX INDEX teldrive.idx_files_name_search;
  END IF;
  IF EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pgroonga')
     AND to_regclass('teldrive.idx_files_name_regex_search') IS NOT NULL THEN
    REINDEX INDEX teldrive.idx_files_name_regex_search;
  END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- No-op: REINDEX is a defensive rebuild, not a schema change to reverse.
