-- The day's summary on the home page (encargo 31): what the platform's AI
-- wrote of each day, per organization and language, each line tagged with
-- the app it is about so a reader sees only lines of apps they may see; and
-- each person's last visit, to show the days since. Only adds.
CREATE TABLE IF NOT EXISTS day_summaries (
    org_id     BIGINT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    day        DATE NOT NULL,
    lang       TEXT NOT NULL,
    lines      JSONB NOT NULL,          -- [{"app": "", "text": ""}]
    source     TEXT NOT NULL,           -- ia, counts (no model) or empty (nothing happened)
    provider   TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, day, lang)
);

CREATE TABLE IF NOT EXISTS summary_visits (
    org_id   BIGINT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    login    TEXT NOT NULL,
    seen_day DATE NOT NULL,
    PRIMARY KEY (org_id, login)
);
