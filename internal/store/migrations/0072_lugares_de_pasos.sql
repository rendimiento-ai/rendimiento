-- Where each step ran (Ruta pendiente 199): its pod's node, or the nodes of
-- the BuildKit daemons that built each half of an image, each with its
-- architecture, as [{"nodo": "...", "arch": "arm64"}]. Only added: the
-- previous version neither reads nor writes it.
ALTER TABLE steps ADD COLUMN IF NOT EXISTS lugares jsonb NOT NULL DEFAULT '[]';
