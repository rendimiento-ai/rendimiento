-- En seco: a branch that changes only its checks (post-deploy tasks, the
-- app's own tests) runs the new ones against the published release, where
-- a failure only warns. runs.dry_release is the release they ran against
-- (NULL: nothing ran en seco), dry_note why nothing ran (no release yet),
-- dry_failed whether one failed (the run is not merged by itself). Each
-- check's result and log is a row of run_dry_tasks. Only adds: the
-- previous version neither reads nor writes them.
ALTER TABLE runs ADD COLUMN IF NOT EXISTS dry_release BIGINT;
ALTER TABLE runs ADD COLUMN IF NOT EXISTS dry_note TEXT NOT NULL DEFAULT '';
ALTER TABLE runs ADD COLUMN IF NOT EXISTS dry_failed BOOLEAN NOT NULL DEFAULT false;

CREATE TABLE IF NOT EXISTS run_dry_tasks (
    run_id      BIGINT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    status      TEXT NOT NULL,              -- running, succeeded, failed, skipped
    message     TEXT NOT NULL DEFAULT '',
    log         TEXT NOT NULL DEFAULT '',   -- the tail, at most 256 KiB
    started_at  TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    PRIMARY KEY (run_id, name)
);
