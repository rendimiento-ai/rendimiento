-- The lookout (el vigía): an agent that reads the platform's log, its runs
-- and the interface's use once a week (or when an administrator asks) and
-- proposes a few findings, each with its evidence. A person decides each
-- one: an encargo, a note in the Ruta, or "not now". What was decided is
-- remembered by the finding's key, so it is not proposed again. Only adds:
-- the previous version neither reads nor writes these tables.
CREATE TABLE IF NOT EXISTS vigia_revisiones (
    id          BIGSERIAL PRIMARY KEY,
    at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    desde       TIMESTAMPTZ NOT NULL,              -- the week it read: desde to at
    pedida_por  TEXT NOT NULL DEFAULT '',          -- '' for the weekly one
    estado      TEXT NOT NULL DEFAULT 'revisando', -- revisando, hecha, fallida
    nota        TEXT NOT NULL DEFAULT '',          -- why it failed, or what it skipped
    proveedor   TEXT NOT NULL DEFAULT '',
    modelo      TEXT NOT NULL DEFAULT '',
    costo_micros BIGINT NOT NULL DEFAULT 0,
    senales     INT NOT NULL DEFAULT 0             -- how many signals the model read
);
CREATE INDEX IF NOT EXISTS vigia_revisiones_at ON vigia_revisiones (at DESC);

CREATE TABLE IF NOT EXISTS vigia_hallazgos (
    id           BIGSERIAL PRIMARY KEY,
    revision_id  BIGINT NOT NULL REFERENCES vigia_revisiones(id) ON DELETE CASCADE,
    clave        TEXT NOT NULL,                    -- the signal it is about: stable from week to week
    titulo       TEXT NOT NULL,
    evidencia    TEXT NOT NULL,
    accionable   BOOLEAN NOT NULL DEFAULT true,    -- false: a fact, with no work suggested
    app          TEXT NOT NULL DEFAULT '',
    estado       TEXT NOT NULL DEFAULT 'nuevo',    -- nuevo, encargo, ruta, descartado
    decidido_por TEXT NOT NULL DEFAULT '',
    decidido_at  TIMESTAMPTZ,
    encargo_id   BIGINT,
    ruta_id      BIGINT
);
CREATE INDEX IF NOT EXISTS vigia_hallazgos_revision ON vigia_hallazgos (revision_id, id);
CREATE INDEX IF NOT EXISTS vigia_hallazgos_clave ON vigia_hallazgos (clave) WHERE estado <> 'nuevo';
