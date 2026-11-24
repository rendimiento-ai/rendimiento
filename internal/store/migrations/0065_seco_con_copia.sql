-- En seco, step 2: a branch that changes the app (or whose checks ask for
-- it, dryRunOn) runs its checks en seco against a copy of the branch: its
-- pull request's, or a short copy made only for them. run_dry_copies is
-- that copy for a run: kind pull (pull is its number) or short, off when
-- the app has no previews, '' when none ran (note says why); its address
-- while it lives (gone once it is deleted); whether a check failed there.
-- Each check's result and log is a row of run_dry_copy_tasks, as
-- run_dry_tasks are against the published release. Only adds: the
-- previous version neither reads nor writes them.
CREATE TABLE IF NOT EXISTS run_dry_copies (
    run_id  BIGINT PRIMARY KEY REFERENCES runs(id) ON DELETE CASCADE,
    kind    TEXT NOT NULL DEFAULT '',      -- pull, short, off, or '' (none ran)
    pull    INTEGER NOT NULL DEFAULT 0,
    url     TEXT NOT NULL DEFAULT '',
    note    TEXT NOT NULL DEFAULT '',
    failed  BOOLEAN NOT NULL DEFAULT false,
    gone    BOOLEAN NOT NULL DEFAULT false
);

CREATE TABLE IF NOT EXISTS run_dry_copy_tasks (
    run_id      BIGINT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    status      TEXT NOT NULL,              -- running, succeeded, failed, skipped
    message     TEXT NOT NULL DEFAULT '',
    log         TEXT NOT NULL DEFAULT '',   -- the tail, at most 256 KiB
    started_at  TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    PRIMARY KEY (run_id, name)
);
