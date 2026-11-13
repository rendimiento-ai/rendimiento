-- The app's own tests, run after each release (decision 136, step 13):
-- what is kept of each test between releases (whether it only reports,
-- blocks, or is set aside; how many releases in a row it passed; how often
-- it failed and passed on its own), and each release's results. Only adds.
CREATE TABLE IF NOT EXISTS app_tests (
    app_id     BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    state      TEXT NOT NULL DEFAULT 'advisory', -- advisory, blocking or quarantined
    streak     INT NOT NULL DEFAULT 0,
    flakes     INT NOT NULL DEFAULT 0,
    last       TEXT NOT NULL DEFAULT '',          -- expected, unexpected, flaky or skipped
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (app_id, name)
);

CREATE TABLE IF NOT EXISTS release_tests (
    release_id BIGINT NOT NULL REFERENCES releases(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    status     TEXT NOT NULL,
    state      TEXT NOT NULL, -- the test's state after this release
    PRIMARY KEY (release_id, name)
);
