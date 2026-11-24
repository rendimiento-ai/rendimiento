-- The coding agent asks the person (Ruta 178): an encargo it stopped on
-- to ask is 'asking' (no work, no spending) until a person answers, it is
-- passed to the person's own agent, or three days go by. The question is
-- kept with what the agent left itself to go on from (notes) and what it
-- had spent, and the answer for when it resumes on the same branch.
-- Only adds: the status list gains a value the previous version never
-- writes (and its queries look only for the ones it knows), and a table it
-- never reads.
ALTER TABLE encargos DROP CONSTRAINT IF EXISTS encargos_status_check;
ALTER TABLE encargos ADD CONSTRAINT encargos_status_check
    CHECK (status IN ('pending', 'taken', 'waiting', 'asking', 'delivered', 'reviewed', 'cancelled'));

CREATE TABLE IF NOT EXISTS encargo_preguntas (
    id           BIGSERIAL PRIMARY KEY,
    encargo_id   BIGINT NOT NULL REFERENCES encargos(id),
    org_id       BIGINT NOT NULL,
    question     TEXT NOT NULL,
    options      TEXT[] NOT NULL DEFAULT '{}',
    notes        TEXT NOT NULL DEFAULT '',
    spent_micros BIGINT NOT NULL DEFAULT 0,
    asked_by     TEXT NOT NULL,
    asked_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    answer       TEXT NOT NULL DEFAULT '',
    answered_by  TEXT NOT NULL DEFAULT '',
    answered_at  TIMESTAMPTZ,
    passed_by    TEXT NOT NULL DEFAULT '',
    passed_at    TIMESTAMPTZ,
    resumed_at   TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS encargo_preguntas_encargo ON encargo_preguntas (encargo_id, id DESC);
CREATE INDEX IF NOT EXISTS encargo_preguntas_abiertas ON encargo_preguntas (asked_at) WHERE answered_at IS NULL AND passed_at IS NULL;
