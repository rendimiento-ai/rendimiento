-- El ayudante (decision 188): what each person asked rendimiento's helper
-- and what it answered, kept for the person alone for 7 days. who is the
-- person's login; kind is "persona" (their question) or "ayudante" (its
-- answer); pagina is where they were; enlaces the links it gave and leido
-- what it read, both JSON. Only adds: the previous version reads none of it.
CREATE TABLE IF NOT EXISTS ayudante_mensajes (
    id          BIGSERIAL PRIMARY KEY,
    org_id      BIGINT NOT NULL,
    who         TEXT NOT NULL,
    kind        TEXT NOT NULL,
    body        TEXT NOT NULL,
    pagina      TEXT NOT NULL DEFAULT '',
    enlaces     JSONB NOT NULL DEFAULT '[]',
    leido       TEXT[] NOT NULL DEFAULT '{}',
    provider    TEXT NOT NULL DEFAULT '',
    model       TEXT NOT NULL DEFAULT '',
    cost_micros BIGINT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS ayudante_mensajes_de ON ayudante_mensajes (org_id, who, created_at);
