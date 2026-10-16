-- +goose Up
-- +goose StatementBegin
DROP INDEX IF EXISTS teldrive.idx_files_name_search;
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_available_extensions WHERE name = 'pgroonga') THEN
    CREATE EXTENSION IF NOT EXISTS pgroonga;
    CREATE INDEX IF NOT EXISTS idx_files_name_search ON teldrive.files USING pgroonga (lower(regexp_replace(name, '[^[:alnum:]\\s]', ' ', 'g'))) WITH (tokenizer='TokenNgram');
    CREATE INDEX IF NOT EXISTS idx_files_name_regex_search ON teldrive.files USING pgroonga (name pgroonga_text_regexp_ops_v2);
  END IF;
END
$$;

-- +goose StatementEnd