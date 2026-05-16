-- +goose Up

-- Files indexes
CREATE INDEX IF NOT EXISTS idx_files_category ON files (user_id, status, category);
CREATE INDEX IF NOT EXISTS idx_files_category_type_user_id ON files (category, type, user_id);
CREATE INDEX IF NOT EXISTS idx_files_created_at ON files (user_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_files_id_browsing ON files (user_id, parent_id, status, id DESC) INCLUDE (name, size, mime_type, type);
CREATE INDEX IF NOT EXISTS idx_files_name_search ON files USING pgroonga (clean_name(name)) WITH (tokenizer='TokenNgram');
CREATE INDEX IF NOT EXISTS idx_files_name_regex_search ON files USING pgroonga (name pgroonga_text_regexp_ops_v2);
CREATE INDEX IF NOT EXISTS idx_files_name_user_id_status ON files (name, user_id, status);
CREATE INDEX IF NOT EXISTS idx_files_parent_id ON files (parent_id);
CREATE INDEX IF NOT EXISTS idx_files_parent_name_lookup ON files (parent_id, name, status);
CREATE INDEX IF NOT EXISTS idx_files_size ON files (user_id, status, size DESC);
CREATE INDEX IF NOT EXISTS idx_files_type ON files (user_id, status, type);
CREATE INDEX IF NOT EXISTS idx_files_updated_at_user_id_status ON files (updated_at DESC, user_id, status);
CREATE INDEX IF NOT EXISTS idx_files_user_active ON files (user_id) WHERE status = 'active';
CREATE INDEX IF NOT EXISTS idx_files_user_channel_type_id ON files (user_id, channel_id, type, id);
CREATE INDEX IF NOT EXISTS idx_files_channel_file_parts ON files (channel_id) WHERE type = 'file';
CREATE INDEX IF NOT EXISTS idx_files_browsing ON files (user_id, parent_id, status, updated_at DESC) INCLUDE (id);
CREATE UNIQUE INDEX IF NOT EXISTS files_unique_active_entry ON files (name, parent_id, user_id) NULLS NOT DISTINCT WHERE status = 'active';

-- Sessions
CREATE UNIQUE INDEX IF NOT EXISTS sessions_refresh_token_hash_unq ON sessions (refresh_token_hash) WHERE refresh_token_hash IS NOT NULL;
CREATE INDEX IF NOT EXISTS sessions_user_created_idx ON sessions (user_id, created_at DESC);

-- File shares
CREATE INDEX IF NOT EXISTS idx_file_shares_file_id ON file_shares (file_id);

-- Events
CREATE INDEX IF NOT EXISTS idx_events_created_at ON events (created_at DESC);

-- API keys
CREATE UNIQUE INDEX IF NOT EXISTS api_keys_token_hash_unq ON api_keys (token_hash);
CREATE INDEX IF NOT EXISTS api_keys_user_created_idx ON api_keys (user_id, created_at DESC) WHERE revoked_at IS NULL;

-- Periodic jobs
CREATE INDEX IF NOT EXISTS idx_periodic_jobs_user_list ON periodic_jobs (user_id, system DESC, created_at ASC);
CREATE INDEX IF NOT EXISTS idx_periodic_jobs_due ON periodic_jobs (next_run_at) WHERE enabled = true;
