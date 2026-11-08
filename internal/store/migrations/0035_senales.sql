-- Señales de vida received (only where SENALES_RECIBIR is on: rendimiento.ai):
-- one row per installation, by its random id, with when it was first and
-- last heard of and its last counts. No address is kept. Only adds.
CREATE TABLE IF NOT EXISTS senales (
    instalacion TEXT PRIMARY KEY,
    primera     TIMESTAMPTZ NOT NULL DEFAULT now(),
    ultima      TIMESTAMPTZ NOT NULL DEFAULT now(),
    equipo      BOOLEAN NOT NULL DEFAULT false,
    envios      INTEGER NOT NULL DEFAULT 1,
    ultimo      JSONB NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS senales_ultima ON senales (ultima DESC);
