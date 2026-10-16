-- +goose Up
-- +goose StatementBegin
DROP INDEX IF EXISTS teldrive.name_search_idx;
DROP FUNCTION IF EXISTS  teldrive.get_tsquery;
DROP FUNCTION IF EXISTS teldrive.get_tsvector;
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_available_extensions WHERE name = 'pgroonga') THEN
    CREATE EXTENSION IF NOT EXISTS pgroonga;
    CREATE INDEX name_search_idx ON teldrive.files USING pgroonga (REGEXP_REPLACE(name, '[.,-_]', ' ', 'g')) WITH (tokenizer = 'TokenNgram');
  END IF;
END
$$;
-- +goose StatementEnd