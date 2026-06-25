-- +goose Up
-- +goose StatementBegin
ALTER TABLE teldrive.files ADD COLUMN IF NOT EXISTS starred BOOLEAN NOT NULL DEFAULT false;
CREATE INDEX IF NOT EXISTS idx_files_starred_updated_at ON teldrive.files USING btree (starred, updated_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS teldrive.idx_files_starred_updated_at;
ALTER TABLE teldrive.files DROP COLUMN IF EXISTS starred;
-- +goose StatementEnd
