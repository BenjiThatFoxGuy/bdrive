CREATE TABLE IF NOT EXISTS teldrive.thumbnails (
  id UUID PRIMARY KEY DEFAULT teldrive.uuid7(),
  file_id UUID NOT NULL REFERENCES teldrive.files(id) ON DELETE CASCADE,
  user_id BIGINT NOT NULL,
  channel_id BIGINT NOT NULL,
  message_id INT NOT NULL,
  encrypted BOOLEAN NOT NULL DEFAULT false,
  salt TEXT NOT NULL DEFAULT '',
  size BIGINT NOT NULL DEFAULT 0,
  mime_type TEXT NOT NULL DEFAULT 'image/jpeg',
  created_at TIMESTAMPTZ NOT NULL DEFAULT (timezone('utc', now())),
  UNIQUE(file_id)
);
CREATE INDEX IF NOT EXISTS idx_thumbnails_file_id ON teldrive.thumbnails(file_id);
