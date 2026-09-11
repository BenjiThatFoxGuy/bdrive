-- +goose Up
CREATE TABLE IF NOT EXISTS teldrive.instance_settings (
    key TEXT PRIMARY KEY,
    value JSONB NOT NULL DEFAULT '{}',
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT timezone('utc'::text, now())
);

-- Seed with empty resizer config
INSERT INTO teldrive.instance_settings (key, value)
VALUES ('thumbnail', '{"resizerHost": "", "resizerWidth": 360, "resizerQuality": 80}')
ON CONFLICT (key) DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS teldrive.instance_settings;
