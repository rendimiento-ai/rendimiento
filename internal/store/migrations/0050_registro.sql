-- The platform's own log in one place: every line at INFO and above from
-- every process (api, worker, control, agents, traces), so a restart no
-- longer loses it and everything can be read together on the Log page.
-- Secrets are removed before a line is written (internal/registro); lines
-- older than 30 days are deleted. Only adds: the previous version neither
-- reads nor writes it.
CREATE TABLE IF NOT EXISTS registro (
    id      BIGSERIAL PRIMARY KEY,
    at      TIMESTAMPTZ NOT NULL,
    level   TEXT NOT NULL,                -- INFO, WARN or ERROR
    process TEXT NOT NULL DEFAULT '',     -- the role: api, control, worker, traces, agents, all, local
    message TEXT NOT NULL,
    app     TEXT NOT NULL DEFAULT '',     -- the record's app attribute
    run_id  BIGINT,                       -- the record's run attribute
    attrs   JSONB NOT NULL DEFAULT '{}'   -- the other attributes, secrets removed
);
CREATE INDEX IF NOT EXISTS registro_at ON registro (at DESC, id DESC);
CREATE INDEX IF NOT EXISTS registro_app_at ON registro (app, at DESC);
