-- The log of every action, by a person or an agent: one row each, chained
-- by hashes per organization (each row carries the previous one's), so a
-- row changed or removed in the database shows (internal/store/bitacora.go).
-- Only adds; agent_actions stays while the Ruta's Activity tab reads it.
CREATE TABLE IF NOT EXISTS bitacora (
    id              BIGSERIAL PRIMARY KEY,
    org_id          BIGINT NOT NULL,
    cuando          TIMESTAMPTZ NOT NULL,
    quien_tipo      TEXT NOT NULL,              -- persona | agente | plataforma
    quien           TEXT NOT NULL,              -- a GitHub login, or an agent key's name
    por             TEXT NOT NULL,              -- interfaz | mcp | plataforma
    accion          TEXT NOT NULL,              -- a route ("POST /api/apps/{app}/rollback") or an MCP tool
    sobre           JSONB NOT NULL DEFAULT '{}',
    resultado       TEXT NOT NULL,
    detalle         JSONB NOT NULL DEFAULT '{}',
    huella_anterior TEXT NOT NULL,
    huella          TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS bitacora_org_id ON bitacora (org_id, id DESC);
