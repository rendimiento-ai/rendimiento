-- Dormir (Ruta 92, decision 177): rendimiento on a computer can sleep (its
-- apps' containers and the CI stop; data, releases and the Ruta stay) and
-- shut down. One row keeps where it stands, so a restart stays asleep:
-- estado is despierto, durmiendose (waiting for a publish to finish) or
-- dormido; contenedores are the containers it stopped, started again on
-- waking; cortadas the runs it cut, repeated on waking; despertar_a a
-- daily hour to wake at ("08:00", '' off); al_arrancar, set by «Apagar del
-- todo», wakes it when it starts again; tapa is «Dormir al cerrar la tapa»
-- (read by the desktop app); resumen what the last wake did.
-- dormir_avisos are the forjas' webhooks received while asleep, handled
-- in order (id) on waking; dormir_periodos the hours it slept, which the
-- lookout does not count.
-- Only adds: the previous version reads none of it.
CREATE TABLE IF NOT EXISTS dormir (
    id             SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    estado         TEXT NOT NULL DEFAULT 'despierto',
    apagar         BOOLEAN NOT NULL DEFAULT false,
    desde          TIMESTAMPTZ,
    por            TEXT NOT NULL DEFAULT '',
    contenedores   TEXT[] NOT NULL DEFAULT '{}',
    cortadas       BIGINT[] NOT NULL DEFAULT '{}',
    esperada       TEXT NOT NULL DEFAULT '',
    despertar_a    TEXT NOT NULL DEFAULT '',
    al_arrancar    BOOLEAN NOT NULL DEFAULT false,
    tapa           BOOLEAN NOT NULL DEFAULT false,
    resumen        JSONB,
    actualizado_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO dormir (id) VALUES (1) ON CONFLICT DO NOTHING;

CREATE TABLE IF NOT EXISTS dormir_avisos (
    id          BIGSERIAL PRIMARY KEY,
    forja       TEXT NOT NULL,
    evento      JSONB NOT NULL,
    recibido_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS dormir_periodos (
    id    BIGSERIAL PRIMARY KEY,
    desde TIMESTAMPTZ NOT NULL,
    hasta TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS dormir_periodos_desde ON dormir_periodos (desde);
