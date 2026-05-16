-- +goose Up

CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS btree_gin;
CREATE EXTENSION IF NOT EXISTS pgroonga;

-- Needed by pgroonga index idx_files_name_search
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION clean_name(val TEXT) RETURNS TEXT LANGUAGE PLPGSQL IMMUTABLE AS $$
BEGIN
  RETURN LOWER(REGEXP_REPLACE(val, '[^[:alnum:]\s]', ' ', 'g'));
END;
$$;
-- +goose StatementEnd

CREATE TABLE IF NOT EXISTS users (
  user_id BIGINT NOT NULL PRIMARY KEY,
  name TEXT,
  user_name TEXT NOT NULL,
  is_premium BOOL NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT TIMEZONE('utc'::text, NOW()),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT TIMEZONE('utc'::text, NOW())
);

CREATE TABLE IF NOT EXISTS channels (
  channel_id BIGINT NOT NULL PRIMARY KEY,
  channel_name TEXT NOT NULL,
  user_id BIGINT NOT NULL REFERENCES users(user_id),
  selected BOOLEAN DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT TIMEZONE('utc'::text, NOW())
);

CREATE TABLE IF NOT EXISTS files (
  id UUID NOT NULL DEFAULT uuidv7() PRIMARY KEY,
  name TEXT NOT NULL,
  type TEXT NOT NULL CHECK (type IN ('file', 'folder')),
  mime_type TEXT NOT NULL,
  size BIGINT,
  user_id BIGINT NOT NULL,
  parent_id UUID,
  status TEXT DEFAULT 'active' CHECK (status IN ('active', 'pending_deletion')),
  channel_id BIGINT,
  parts JSONB,
  encrypted BOOL NOT NULL DEFAULT FALSE,
  category TEXT,
  hash TEXT,
  created_at TIMESTAMP NOT NULL DEFAULT TIMEZONE('utc'::text, NOW()),
  updated_at TIMESTAMP NOT NULL DEFAULT TIMEZONE('utc'::text, NOW())
);

CREATE TABLE IF NOT EXISTS uploads (
  upload_id TEXT NOT NULL,
  name TEXT NOT NULL,
  user_id BIGINT,
  part_no INT NOT NULL,
  part_id INT NOT NULL,
  channel_id BIGINT NOT NULL,
  size BIGINT NOT NULL,
  encrypted BOOL NOT NULL DEFAULT FALSE,
  salt TEXT,
  block_hashes BYTEA,
  created_at TIMESTAMP DEFAULT TIMEZONE('utc'::text, NOW()),
  CONSTRAINT part_id_greater_than_zero CHECK (part_id > 0),
  PRIMARY KEY (part_id, channel_id)
);

CREATE TABLE IF NOT EXISTS sessions (
  id UUID NOT NULL PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(user_id),
  tg_session TEXT NOT NULL,
  session_date INTEGER,
  refresh_token_hash TEXT,
  created_at TIMESTAMP NOT NULL DEFAULT TIMEZONE('utc'::text, NOW()),
  updated_at TIMESTAMP NOT NULL DEFAULT TIMEZONE('utc'::text, NOW()),
  revoked_at TIMESTAMP
);

CREATE TABLE IF NOT EXISTS bots (
  user_id BIGINT NOT NULL REFERENCES users(user_id),
  token TEXT NOT NULL,
  bot_id BIGINT NOT NULL,
  CONSTRAINT bots_pkey PRIMARY KEY (user_id, token)
);

CREATE TABLE IF NOT EXISTS kv (
  key TEXT PRIMARY KEY,
  value BYTEA NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT TIMEZONE('utc'::text, NOW())
);

CREATE TABLE IF NOT EXISTS file_shares (
  id UUID NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
  file_id UUID NOT NULL REFERENCES files(id) ON DELETE CASCADE,
  password TEXT,
  expires_at TIMESTAMP,
  user_id BIGINT NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT TIMEZONE('utc'::text, NOW()),
  updated_at TIMESTAMP NOT NULL DEFAULT TIMEZONE('utc'::text, NOW())
);

CREATE TABLE IF NOT EXISTS events (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  type TEXT NOT NULL,
  user_id BIGINT NOT NULL,
  source JSONB,
  created_at TIMESTAMP NOT NULL DEFAULT TIMEZONE('utc'::text, NOW())
);

CREATE TABLE IF NOT EXISTS api_keys (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id BIGINT NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  token_hash TEXT NOT NULL,
  expires_at TIMESTAMP,
  last_used_at TIMESTAMP,
  revoked_at TIMESTAMP,
  created_at TIMESTAMP NOT NULL DEFAULT TIMEZONE('utc'::text, NOW()),
  updated_at TIMESTAMP NOT NULL DEFAULT TIMEZONE('utc'::text, NOW())
);

CREATE TABLE IF NOT EXISTS periodic_jobs (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id BIGINT NOT NULL,
  name TEXT NOT NULL,
  kind TEXT NOT NULL,
  args JSONB NOT NULL DEFAULT '{}'::jsonb,
  cron_expression TEXT NOT NULL,
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  system BOOLEAN NOT NULL DEFAULT FALSE,
  next_run_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_run_at TIMESTAMPTZ,
  last_state TEXT NOT NULL DEFAULT 'idle' CHECK (last_state IN ('idle', 'running', 'succeeded', 'failed')),
  last_error TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT periodic_jobs_name_unique UNIQUE (user_id, name),
  CONSTRAINT periodic_jobs_cron_check CHECK (btrim(cron_expression) <> '')
);
