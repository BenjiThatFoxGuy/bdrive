-- +goose Up
-- Defensive rebuild of the pgroonga indexes on teldrive.files.name. Repeated
-- "grn_io_lock failed" contention (see internal/database.IsTransientLockErr)
-- from bursts of writes during dedup runs stresses pgroonga's index storage,
-- which lives outside Postgres's normal heap/WAL. A REINDEX guarantees both
-- indexes are rebuilt cleanly from the current table contents.
REINDEX INDEX teldrive.idx_files_name_search;
REINDEX INDEX teldrive.idx_files_name_regex_search;

-- +goose Down
-- No-op: REINDEX is a defensive rebuild, not a schema change to reverse.
