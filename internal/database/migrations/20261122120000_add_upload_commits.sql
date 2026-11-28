-- +goose Up
-- +goose StatementBegin
-- A durable record of what a completed upload session produced. Upload rows
-- are deleted when the file is created, so without this a client whose create
-- response was lost has no way to tell that its bytes are already on Telegram
-- and re-sends them, leaving duplicate messages in the storage channel.
CREATE TABLE IF NOT EXISTS teldrive.upload_commits (
    user_id bigint NOT NULL,
    upload_id text NOT NULL,
    file_id uuid NOT NULL,
    file_size bigint NOT NULL,
    parts jsonb NOT NULL,
    created_at timestamp DEFAULT timezone('utc'::text, now()),
    PRIMARY KEY (user_id, upload_id)
);

CREATE INDEX IF NOT EXISTS idx_upload_commits_file_id
    ON teldrive.upload_commits USING btree (file_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS teldrive.idx_upload_commits_file_id;
DROP TABLE IF EXISTS teldrive.upload_commits;
-- +goose StatementEnd
