-- +goose Up
-- +goose StatementBegin
ALTER TABLE teldrive.file_shares ADD COLUMN IF NOT EXISTS short_code TEXT;
ALTER TABLE teldrive.file_shares ADD COLUMN IF NOT EXISTS block_direct_link BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE teldrive.file_shares ADD COLUMN IF NOT EXISTS always_direct_link BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE teldrive.file_shares ADD COLUMN IF NOT EXISTS allow_zip_download BOOLEAN NOT NULL DEFAULT false;
CREATE UNIQUE INDEX IF NOT EXISTS idx_file_shares_short_code ON teldrive.file_shares USING btree (short_code);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS teldrive.idx_file_shares_short_code;
ALTER TABLE teldrive.file_shares DROP COLUMN IF EXISTS allow_zip_download;
ALTER TABLE teldrive.file_shares DROP COLUMN IF EXISTS always_direct_link;
ALTER TABLE teldrive.file_shares DROP COLUMN IF EXISTS block_direct_link;
ALTER TABLE teldrive.file_shares DROP COLUMN IF EXISTS short_code;
-- +goose StatementEnd
