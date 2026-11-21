-- The keys changed on the Environment page (Llaves y ajustes): which key,
-- who changed it, when, and when it expires if its service says. Never the
-- value: that lives in the Kubernetes Secret where it is used. version
-- goes up with each change, so every process of the platform notices it
-- (within 15 seconds) and reads the new value from its Secret, with no
-- restart. Only adds: the previous version neither reads nor writes it.
CREATE TABLE IF NOT EXISTS llaves (
    llave        TEXT PRIMARY KEY,          -- cloudflare, correo, ia
    version      BIGINT NOT NULL DEFAULT 1,
    cambiada_por TEXT NOT NULL,
    cambiada_en  TIMESTAMPTZ NOT NULL DEFAULT now(),
    vence_en     TIMESTAMPTZ                -- NULL: it does not expire, or nobody says
);
