-- The coding agent's run tool (Ruta 178): whether an organization lets it
-- run commands in a sandbox pod (code_run, on by default). Only adds, with
-- a default: the previous version neither reads nor writes it. What it ran
-- goes to encargo_log as 'ran', a value the previous version shows as is.
ALTER TABLE ai_settings ADD COLUMN IF NOT EXISTS code_run BOOLEAN NOT NULL DEFAULT true;
