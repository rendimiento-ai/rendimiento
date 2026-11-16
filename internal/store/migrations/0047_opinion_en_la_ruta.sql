-- A comment a person passed to the Ruta: the entry it became, so it is
-- passed once and its page links to it. Only adds.
ALTER TABLE opiniones ADD COLUMN IF NOT EXISTS ruta_id BIGINT REFERENCES ruta_entries(id) ON DELETE SET NULL;
