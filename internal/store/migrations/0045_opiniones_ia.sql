-- What the platform's AI makes of each comment (decision 145, step 2):
-- what it matches (book pages, Ruta entries, earlier comments), the earlier
-- comment it groups with (the count of a group is the evidence of demand),
-- and when it was classified, or why it could not be. Only adds.
ALTER TABLE opiniones ADD COLUMN IF NOT EXISTS coincidencias JSONB NOT NULL DEFAULT '[]';
ALTER TABLE opiniones ADD COLUMN IF NOT EXISTS grupo BIGINT REFERENCES opiniones(id) ON DELETE SET NULL;
ALTER TABLE opiniones ADD COLUMN IF NOT EXISTS clasificada_at TIMESTAMPTZ;
ALTER TABLE opiniones ADD COLUMN IF NOT EXISTS ia_error TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS opiniones_sin_clasificar ON opiniones (id) WHERE clasificada_at IS NULL;
