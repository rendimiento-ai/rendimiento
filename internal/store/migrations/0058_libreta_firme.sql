-- The libreta keeps what it learns (Ruta 150): when a branch linked to an
-- entry was merged (its head commit and when) and the release that carried
-- it, written when rendimiento sees the merge and when that release passes
-- its verification, so an old request still says where it went once its
-- runs are past the window the libreta reads. And the Ruta's history keeps
-- an entry's app and why it was set aside, like its other fields. Only
-- adds: the previous version names its columns and never reads these.
ALTER TABLE ruta_ramas ADD COLUMN IF NOT EXISTS merged_sha TEXT;
ALTER TABLE ruta_ramas ADD COLUMN IF NOT EXISTS merged_at TIMESTAMPTZ;
ALTER TABLE ruta_ramas ADD COLUMN IF NOT EXISTS release_number BIGINT;
ALTER TABLE ruta_ramas ADD COLUMN IF NOT EXISTS released_at TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS ruta_ramas_branch ON ruta_ramas (branch);

ALTER TABLE ruta_history ADD COLUMN IF NOT EXISTS app TEXT;
ALTER TABLE ruta_history ADD COLUMN IF NOT EXISTS discarded TEXT;
