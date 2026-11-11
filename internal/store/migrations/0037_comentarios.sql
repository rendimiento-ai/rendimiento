-- Comentarios: what people say about an agent's work, once it is done or
-- while it goes on, without sending it back: on a handed-over cargo or on
-- an encargo. Agents read them (ruta_inicio, encargo_leer) and answer; an
-- answer reaches the person who commented in the bell. Only adds.
CREATE TABLE IF NOT EXISTS comentarios (
    id          BIGSERIAL PRIMARY KEY,
    org_id      BIGINT NOT NULL REFERENCES orgs(id),
    cargo_id    BIGINT REFERENCES cargos(id),
    encargo_id  BIGINT REFERENCES encargos(id),
    responde_a  BIGINT REFERENCES comentarios(id), -- an agent's answer: the comment it answers
    author      TEXT NOT NULL,                     -- GitHub login, or the agent key's name
    by_agent    BOOLEAN NOT NULL DEFAULT false,
    key_id      BIGINT REFERENCES agent_keys(id),  -- the agent that answered
    body        TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    answered_at TIMESTAMPTZ,                       -- a person's comment: when an agent answered it
    seen_at     TIMESTAMPTZ,                       -- an agent's answer: when the person saw it
    CHECK ((cargo_id IS NULL) <> (encargo_id IS NULL))
);
CREATE INDEX IF NOT EXISTS comentarios_cargo ON comentarios (cargo_id, created_at) WHERE cargo_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS comentarios_encargo ON comentarios (encargo_id, created_at) WHERE encargo_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS comentarios_sin_responder ON comentarios (org_id, created_at) WHERE NOT by_agent AND answered_at IS NULL;
