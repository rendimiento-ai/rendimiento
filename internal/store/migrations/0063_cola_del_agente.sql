-- The coding agent's queue (la Ruta nueva): the order a person gave its
-- encargos (queue_pos; NULL: after the ordered ones, oldest first), what it
-- is doing now on the one it has (doing: "tool:what", '' when nothing),
-- whether a person asked it to stop (stop_asked), and how many encargos
-- an organization lets it do at once (code_concurrency, never two of the
-- same app). Only adds, with defaults: the previous version neither reads
-- nor writes them, and does one encargo at a time as before.
ALTER TABLE encargos ADD COLUMN IF NOT EXISTS queue_pos INTEGER;
ALTER TABLE encargos ADD COLUMN IF NOT EXISTS doing TEXT NOT NULL DEFAULT '';
ALTER TABLE encargos ADD COLUMN IF NOT EXISTS stop_asked BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE ai_settings ADD COLUMN IF NOT EXISTS code_concurrency INTEGER NOT NULL DEFAULT 1;
