-- Changes to production an agent asks for and a person decides: an agent
-- may ask (produccion_pedir), never do; an administrator approves or
-- rejects, and the platform then does it in that person's name. Only adds.
CREATE TABLE IF NOT EXISTS aprobaciones (
    id           BIGSERIAL PRIMARY KEY,
    org_id       BIGINT NOT NULL,
    app          TEXT NOT NULL,
    accion       TEXT NOT NULL,              -- revertir
    version      BIGINT NOT NULL,            -- the release to go back to
    motivo       TEXT NOT NULL,
    pedida_por   TEXT NOT NULL,              -- the agent key's name
    key_id       BIGINT NOT NULL,
    estado       TEXT NOT NULL DEFAULT 'pendiente', -- pendiente | aprobada | rechazada | fallida (vencida is computed)
    decidida_por TEXT NOT NULL DEFAULT '',
    comentario   TEXT NOT NULL DEFAULT '',
    resultado    TEXT NOT NULL DEFAULT '',
    creada       TIMESTAMPTZ NOT NULL DEFAULT now(),
    decidida     TIMESTAMPTZ,
    vence        TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS aprobaciones_org ON aprobaciones (org_id, id DESC);
