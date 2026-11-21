-- Platicar una entrada: a conversation about one entry of the Ruta, kept
-- with it (one per entry), between the people who can see it and the AI
-- they choose (or their own agent, over MCP). Each message is signed by
-- who wrote it: a person's login, the AI's provider and model, or the
-- agent key's name. «La idea hasta ahora» is the summary the AI keeps
-- (idea), which people may correct; spent_micros is what its AI cost, and
-- allowed_micros what people allowed past the organization's cap per
-- conversation (ai_settings.platica_cap_micros, 50 cents by default).
-- Only adds: the previous version reads none of it.
CREATE TABLE IF NOT EXISTS platicas (
    id             BIGSERIAL PRIMARY KEY,
    org_id         BIGINT NOT NULL,
    entry_id       BIGINT NOT NULL UNIQUE REFERENCES ruta_entries(id),
    ia             TEXT NOT NULL DEFAULT '',
    idea           TEXT NOT NULL DEFAULT '',
    idea_by        TEXT NOT NULL DEFAULT '',
    idea_at        TIMESTAMPTZ,
    spent_micros   BIGINT NOT NULL DEFAULT 0,
    allowed_micros BIGINT NOT NULL DEFAULT 0,
    thinking_since TIMESTAMPTZ,
    decision_id    BIGINT,
    encargos       BIGINT[] NOT NULL DEFAULT '{}',
    created_by     TEXT NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS platicas_org ON platicas (org_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS platica_mensajes (
    id          BIGSERIAL PRIMARY KEY,
    platica_id  BIGINT NOT NULL REFERENCES platicas(id),
    author      TEXT NOT NULL,
    kind        TEXT NOT NULL,
    provider    TEXT NOT NULL DEFAULT '',
    model       TEXT NOT NULL DEFAULT '',
    key_id      BIGINT,
    body        TEXT NOT NULL,
    read        TEXT[] NOT NULL DEFAULT '{}',
    cost_micros BIGINT NOT NULL DEFAULT 0,
    note        TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS platica_mensajes_platica ON platica_mensajes (platica_id, id);

ALTER TABLE ai_settings ADD COLUMN IF NOT EXISTS platica_cap_micros BIGINT NOT NULL DEFAULT 500000;
