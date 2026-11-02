ALTER TABLE daily_metrics ADD COLUMN IF NOT EXISTS custom_events bigint NOT NULL DEFAULT 0;
ALTER TABLE events ADD COLUMN IF NOT EXISTS event_name text NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS events_custom_app_day ON events(app_id, day) WHERE event_type = 'custom';
ALTER TABLE apps ADD COLUMN IF NOT EXISTS allowed_path_prefixes text[] NOT NULL DEFAULT '{}';
ALTER TABLE apps ADD COLUMN IF NOT EXISTS normalize_numeric_paths boolean NOT NULL DEFAULT FALSE;
CREATE TABLE IF NOT EXISTS operation_metrics (
    app_id text NOT NULL REFERENCES apps(id),
    day date NOT NULL,
    requests bigint NOT NULL DEFAULT 0,
    accepted bigint NOT NULL DEFAULT 0,
    duplicates bigint NOT NULL DEFAULT 0,
    rejected bigint NOT NULL DEFAULT 0,
    rate_limited bigint NOT NULL DEFAULT 0,
    PRIMARY KEY(app_id, day)
);
CREATE INDEX IF NOT EXISTS operation_metrics_day ON operation_metrics(day);

-- The exclusive cutoff stays monotonic even after retention is disabled.
CREATE TABLE IF NOT EXISTS retention_state (
    id integer PRIMARY KEY CHECK (id = 1),
    cutoff date
);
INSERT INTO retention_state(id) VALUES(1) ON CONFLICT DO NOTHING;
