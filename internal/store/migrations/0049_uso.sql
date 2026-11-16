-- How rendimiento's own interface is used, as totals per day (UTC) and
-- never per person: no login, session or address is kept, only the page's
-- route pattern ("/apps/:name/runs/:id"), the one it came from, the main
-- actions by a short fixed name, and how long pages took to show their
-- data (a count, a sum and five buckets). Rows older than 90 days are
-- deleted. Only adds.
CREATE TABLE IF NOT EXISTS uso_paginas (
    dia      DATE   NOT NULL,
    ruta     TEXT   NOT NULL,
    vistas   BIGINT NOT NULL DEFAULT 0,
    medidas  BIGINT NOT NULL DEFAULT 0, -- views whose time to data was measured
    ms_total BIGINT NOT NULL DEFAULT 0,
    menos_500  BIGINT NOT NULL DEFAULT 0,
    menos_1000 BIGINT NOT NULL DEFAULT 0,
    menos_2000 BIGINT NOT NULL DEFAULT 0,
    menos_4000 BIGINT NOT NULL DEFAULT 0,
    mas_4000   BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (dia, ruta)
);
CREATE TABLE IF NOT EXISTS uso_pasos (
    dia   DATE   NOT NULL,
    desde TEXT   NOT NULL, -- '' on the first page of a visit
    hacia TEXT   NOT NULL,
    veces BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (dia, desde, hacia)
);
CREATE TABLE IF NOT EXISTS uso_acciones (
    dia    DATE   NOT NULL,
    ruta   TEXT   NOT NULL,
    accion TEXT   NOT NULL,
    veces  BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (dia, ruta, accion)
);
