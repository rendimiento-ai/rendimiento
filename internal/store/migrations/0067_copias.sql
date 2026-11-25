-- The copies on the app's Caminos tab (Ruta 180): one row per copy's life,
-- a pull request's (pull) or a short one en seco (seco_run), from when it
-- was made until it was deleted (gone_at). What the cluster's App object
-- or the computer's containers do not keep: the run that made it, its own
-- tests' result, the day a person moved its deletion to (keep_until), and
-- why it went (gone_why: merged, closed, idle, max, person, app, seco) and
-- who deleted it. Only adds: the previous version neither reads nor
-- writes it.
CREATE TABLE IF NOT EXISTS copias (
    id           BIGSERIAL PRIMARY KEY,
    app_id       BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    pull         INTEGER NOT NULL DEFAULT 0,
    seco_run     BIGINT NOT NULL DEFAULT 0,
    run_id       BIGINT NOT NULL DEFAULT 0,
    branch       TEXT NOT NULL DEFAULT '',
    made_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    keep_until   TIMESTAMPTZ,
    tests_passed INTEGER,
    tests_total  INTEGER,
    tests_failed TEXT[] NOT NULL DEFAULT '{}',
    gone_at      TIMESTAMPTZ,
    gone_why     TEXT NOT NULL DEFAULT '',
    gone_note    TEXT NOT NULL DEFAULT '',
    gone_by      TEXT NOT NULL DEFAULT ''
);

-- One live row per copy.
CREATE UNIQUE INDEX IF NOT EXISTS copias_viva ON copias (app_id, pull, seco_run) WHERE gone_at IS NULL;
CREATE INDEX IF NOT EXISTS copias_idas ON copias (app_id, gone_at DESC);
