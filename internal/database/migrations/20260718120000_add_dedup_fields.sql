-- +goose Up
-- +goose StatementBegin

-- Add referenced_file_id column for deduplication support
ALTER TABLE teldrive.files ADD COLUMN IF NOT EXISTS referenced_file_id UUID;

-- Create index on referenced_file_id for efficient lookups
CREATE INDEX IF NOT EXISTS idx_files_referenced_file_id ON teldrive.files USING btree (referenced_file_id);

-- Create composite index on (user_id, hash) for efficient duplicate detection
-- This allows fast queries like: SELECT * FROM files WHERE user_id = ? AND hash = ? AND encrypted = false
CREATE INDEX IF NOT EXISTS idx_files_user_id_hash ON teldrive.files USING btree (user_id, hash) WHERE hash IS NOT NULL AND encrypted = false;

-- Add foreign key constraint to referenced_file_id
-- ON DELETE SET NULL ensures that if the canonical file is deleted, copies are orphaned (not auto-deleted)
ALTER TABLE teldrive.files ADD CONSTRAINT fk_files_referenced_file_id 
FOREIGN KEY (referenced_file_id) REFERENCES teldrive.files(id) ON DELETE SET NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Drop the foreign key constraint first
ALTER TABLE teldrive.files DROP CONSTRAINT IF EXISTS fk_files_referenced_file_id;

-- Drop all indexes
DROP INDEX IF EXISTS teldrive.idx_files_referenced_file_id;
DROP INDEX IF EXISTS teldrive.idx_files_user_id_hash;

-- Drop the column
ALTER TABLE teldrive.files DROP COLUMN IF EXISTS referenced_file_id;

-- +goose StatementEnd
