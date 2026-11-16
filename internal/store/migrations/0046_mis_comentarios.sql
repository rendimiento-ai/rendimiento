-- The comments people of this installation wrote to the team (decision
-- 145, step 4), kept here: the text, who wrote it (it never leaves), the
-- number rendimiento.ai gave it, the token that alone fetches its answer
-- (only its hash went out), and the answer once it came, with whether its
-- writer saw it. Only adds.
CREATE TABLE IF NOT EXISTS mis_comentarios (
    id            BIGSERIAL PRIMARY KEY,
    login         TEXT NOT NULL,
    texto         TEXT NOT NULL,
    remoto        BIGINT NOT NULL,
    ficha         TEXT NOT NULL,
    respuesta     TEXT NOT NULL DEFAULT '',
    respondido_por TEXT NOT NULL DEFAULT '',
    respondido_at TIMESTAMPTZ,
    visto         BOOLEAN NOT NULL DEFAULT false,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS mis_comentarios_login ON mis_comentarios (login, created_at);
