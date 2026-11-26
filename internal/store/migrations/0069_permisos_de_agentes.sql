-- What each agent may do (Ruta 184). A subject is an agent: one of the
-- platform's own ("coder", "diagnostician", "estimator", ...) or a
-- person's MCP key ("mcp:<key id>"). A permission without a row has its
-- default (store.PermisoDefault). A request is what an agent asked a person
-- for, and a grant what a person gave: once, while an encargo (or the key's
-- cargo) lasts, or always for one app. Only adds: the previous version
-- neither reads nor writes these tables, and ai_settings.code_run stays,
-- kept in step with the coding agent's sandbox permission, for it.
CREATE TABLE IF NOT EXISTS agente_permisos (
    org_id     BIGINT NOT NULL,
    sujeto     TEXT NOT NULL,
    permiso    TEXT NOT NULL,
    modo       TEXT NOT NULL CHECK (modo IN ('off', 'ask', 'always')),
    apps       TEXT[] NOT NULL DEFAULT '{}',
    updated_by TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, sujeto, permiso)
);

CREATE TABLE IF NOT EXISTS agente_permiso_pedidos (
    id          BIGSERIAL PRIMARY KEY,
    org_id      BIGINT NOT NULL,
    sujeto      TEXT NOT NULL,
    permiso     TEXT NOT NULL,
    app         TEXT NOT NULL,
    tool        TEXT NOT NULL DEFAULT '',
    accion      TEXT NOT NULL DEFAULT '',
    que         TEXT NOT NULL DEFAULT '',
    por_que     TEXT NOT NULL DEFAULT '',
    ventana     TEXT NOT NULL DEFAULT '',
    encargo_id  BIGINT,
    cargo_id    BIGINT,
    asked_by    TEXT NOT NULL,
    asked_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    answer      TEXT NOT NULL DEFAULT '',
    answered_by TEXT NOT NULL DEFAULT '',
    answered_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS agente_permiso_pedidos_abiertos ON agente_permiso_pedidos (org_id, asked_at) WHERE answered_at IS NULL;

CREATE TABLE IF NOT EXISTS agente_permiso_dados (
    id         BIGSERIAL PRIMARY KEY,
    org_id     BIGINT NOT NULL,
    sujeto     TEXT NOT NULL,
    permiso    TEXT NOT NULL,
    app        TEXT NOT NULL,
    alcance    TEXT NOT NULL CHECK (alcance IN ('once', 'encargo', 'cargo', 'always')),
    accion     TEXT NOT NULL DEFAULT '',
    encargo_id BIGINT,
    cargo_id   BIGINT,
    pedido_id  BIGINT,
    granted_by TEXT NOT NULL,
    granted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    used_at    TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    revoked_by TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS agente_permiso_dados_vivos ON agente_permiso_dados (org_id, sujeto, permiso) WHERE revoked_at IS NULL;

-- The coding agent's question that is a request for a permission.
ALTER TABLE encargo_preguntas ADD COLUMN IF NOT EXISTS pedido_id BIGINT;

-- The run tool's switch becomes the coding agent's sandbox permission.
INSERT INTO agente_permisos (org_id, sujeto, permiso, modo, updated_by)
    SELECT org_id, 'coder', 'sandbox', 'off', COALESCE(NULLIF(updated_by, ''), 'rendimiento') FROM ai_settings WHERE code_run = false
    ON CONFLICT DO NOTHING;
