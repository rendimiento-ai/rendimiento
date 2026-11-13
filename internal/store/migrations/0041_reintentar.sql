-- Retrying what failed (encargo 29): how often a run was asked to go again
-- (retries; attempts stays for retries after a platform restart), how often
-- each step ran (tries), and the stumbles: steps that failed and then passed
-- on the same commit, to tell a flaky step. Only adds: the previous version
-- ignores all of it.
ALTER TABLE runs ADD COLUMN IF NOT EXISTS retries INT NOT NULL DEFAULT 0;
ALTER TABLE steps ADD COLUMN IF NOT EXISTS tries INT NOT NULL DEFAULT 1;

CREATE TABLE IF NOT EXISTS step_stumbles (
    id      BIGSERIAL PRIMARY KEY,
    app_id  BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    step_id TEXT NOT NULL,
    run_id  BIGINT NOT NULL,
    at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS step_stumbles_app_step ON step_stumbles (app_id, step_id, at);
