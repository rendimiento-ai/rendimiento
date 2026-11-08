-- An agent's reason for a request, put in the reader's language the first
-- time someone reads it in another one, and kept ({"en": "…"}). Only adds.
ALTER TABLE aprobaciones ADD COLUMN IF NOT EXISTS traducciones JSONB NOT NULL DEFAULT '{}';
