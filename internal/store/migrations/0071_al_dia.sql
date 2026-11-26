-- Poner al día (decision 191): the report on each run of a dependency
-- update's pull request (Renovate's, Dependabot's) and each organization's
-- choices for it. A report is asked for when the run ends (estado
-- pendiente), written by the agents (listo, sin_cambios or fallo), and its
-- encargo is the one that puts the app up to date. notas is the pull
-- request's text (its release notes); falla what would fail; cambia the
-- files it would change, as JSON {archivo, mas, menos}. Only adds: the
-- previous version reads none of it.
CREATE TABLE IF NOT EXISTS al_dia (
    id             BIGSERIAL PRIMARY KEY,
    org_id         BIGINT NOT NULL,
    app_id         BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    pull           INT NOT NULL,
    titulo         TEXT NOT NULL DEFAULT '',
    notas          TEXT NOT NULL DEFAULT '',
    branch         TEXT NOT NULL,
    sha            TEXT NOT NULL,
    run_id         BIGINT NOT NULL,
    run_failed     BOOLEAN NOT NULL DEFAULT false,
    estado         TEXT NOT NULL DEFAULT 'pendiente',
    falla          JSONB NOT NULL DEFAULT '[]',
    cambia         JSONB NOT NULL DEFAULT '[]',
    riesgo         TEXT NOT NULL DEFAULT '',
    dificultad     INT NOT NULL DEFAULT 0,
    costo_micros   BIGINT NOT NULL DEFAULT 0,
    resumen        TEXT NOT NULL DEFAULT '',
    nota           TEXT NOT NULL DEFAULT '',
    provider       TEXT NOT NULL DEFAULT '',
    model          TEXT NOT NULL DEFAULT '',
    informe_micros BIGINT NOT NULL DEFAULT 0,
    encargo_id     BIGINT,
    puesto_por     TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (run_id)
);
CREATE INDEX IF NOT EXISTS al_dia_pendientes ON al_dia (created_at) WHERE estado = 'pendiente';
CREATE INDEX IF NOT EXISTS al_dia_de_app ON al_dia (app_id, pull, id DESC);

CREATE TABLE IF NOT EXISTS al_dia_ajustes (
    org_id      BIGINT PRIMARY KEY,
    encendido   BOOLEAN NOT NULL DEFAULT true,
    quien       TEXT NOT NULL DEFAULT 'platform',
    cuando      TEXT NOT NULL DEFAULT 'toda',
    sola        BOOLEAN NOT NULL DEFAULT false,
    tope_micros BIGINT NOT NULL DEFAULT 250000,
    updated_by  TEXT NOT NULL DEFAULT '',
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
