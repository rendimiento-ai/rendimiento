-- La libreta of each app (Ruta 150): what was asked for an app and whether
-- it is done, a view of the Ruta and the encargos, never a second memory.
-- A Ruta entry may now be about one app (app, NULL for none), may be set
-- aside with its reason (discarded, NULL while it stands), and may name the
-- branches it is worked on (ruta_ramas), so its state is computed from
-- their runs, merges and releases. Only adds: the previous version names
-- its columns and never reads these.
ALTER TABLE ruta_entries ADD COLUMN IF NOT EXISTS app TEXT;
ALTER TABLE ruta_entries ADD COLUMN IF NOT EXISTS discarded TEXT;
CREATE INDEX IF NOT EXISTS ruta_entries_app ON ruta_entries (org_id, app) WHERE app IS NOT NULL;

CREATE TABLE IF NOT EXISTS ruta_ramas (
    entry_id   BIGINT NOT NULL REFERENCES ruta_entries(id) ON DELETE CASCADE,
    branch     TEXT NOT NULL,
    created_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (entry_id, branch)
);
