-- The runs of the apps' declared fixes (rendimiento.yaml's fixes:): which
-- fix, on which branch and commit, who asked (a person's login, the coding
-- agent for an encargo, or Renovate's branch by itself), how it ended,
-- the files it wrote and its whole log. One a commit for Renovate's: the
-- unique index keeps rendimiento from fixing the same commit twice. Only
-- adds: the previous version neither reads nor writes it.
CREATE TABLE IF NOT EXISTS fix_runs (
    id          BIGSERIAL PRIMARY KEY,
    app_id      BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    branch      TEXT NOT NULL,
    sha         TEXT NOT NULL DEFAULT '',  -- the commit it ran on
    fix         TEXT NOT NULL,             -- the fix's name
    asked_kind  TEXT NOT NULL,             -- person, agent, renovate
    asked_by    TEXT NOT NULL DEFAULT '',  -- a login, or the agent's name
    encargo_id  BIGINT,                    -- the coding agent's encargo
    run_id      BIGINT,                    -- the failed run it answers, if any
    status      TEXT NOT NULL DEFAULT 'running', -- running, succeeded, failed, unchanged
    message     TEXT NOT NULL DEFAULT '',
    image       TEXT NOT NULL DEFAULT '',
    image_from  TEXT NOT NULL DEFAULT '',
    files       TEXT[] NOT NULL DEFAULT '{}',   -- what it wrote
    ignored     TEXT[] NOT NULL DEFAULT '{}',   -- what it changed and may not write
    commit_sha  TEXT NOT NULL DEFAULT '',  -- the commit of its files, when it made one
    log         TEXT NOT NULL DEFAULT '',
    log_ref     TEXT NOT NULL DEFAULT '',  -- where the log went, once archived
    started_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS fix_runs_app ON fix_runs (app_id, id DESC);
CREATE UNIQUE INDEX IF NOT EXISTS fix_runs_renovate_once ON fix_runs (app_id, sha, fix) WHERE asked_kind = 'renovate';
