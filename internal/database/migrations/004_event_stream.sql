-- +goose Up
ALTER TABLE events ADD COLUMN IF NOT EXISTS seq BIGSERIAL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_events_seq ON events (seq);
CREATE INDEX IF NOT EXISTS idx_events_user_seq ON events (user_id, seq);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION notify_event_changed()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    PERFORM pg_notify('teldrive_events', NEW.seq::text);
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

DROP TRIGGER IF EXISTS trg_event_changed ON events;

CREATE TRIGGER trg_event_changed
    AFTER INSERT
    ON events
    FOR EACH ROW
    EXECUTE FUNCTION notify_event_changed();

-- +goose Down
DROP TRIGGER IF EXISTS trg_event_changed ON events;
DROP FUNCTION IF EXISTS notify_event_changed();
DROP INDEX IF EXISTS idx_events_user_seq;
DROP INDEX IF EXISTS idx_events_seq;
ALTER TABLE events DROP COLUMN IF EXISTS seq;
