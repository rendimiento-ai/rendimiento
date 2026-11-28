-- When each node «a ratos» was on (Ruta pendiente 199): one row per stretch,
-- hasta empty while it is still on. The drawer of the node on the
-- Environment page counts its hours this week from here. Only added: the
-- previous version neither reads nor writes it.
CREATE TABLE IF NOT EXISTS nodos_encendidos (
    nodo  TEXT NOT NULL,
    desde TIMESTAMPTZ NOT NULL DEFAULT now(),
    hasta TIMESTAMPTZ
);
CREATE UNIQUE INDEX IF NOT EXISTS nodos_encendidos_abierto ON nodos_encendidos (nodo) WHERE hasta IS NULL;
CREATE INDEX IF NOT EXISTS nodos_encendidos_nodo ON nodos_encendidos (nodo, desde DESC);
-- The steps that ran on a node: lugares is [{"nodo": ..., "arch": ...}].
CREATE INDEX IF NOT EXISTS steps_lugares ON steps USING gin (lugares jsonb_path_ops);
