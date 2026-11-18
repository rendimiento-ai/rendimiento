-- Encargos that learn (Ruta 77). The coding agent gets a fourth mode,
-- system: it takes by itself the cheap encargos about a run (fixing a
-- failed run, resolving a conflict), which rendimiento opens, and leaves
-- the ones people write for them to send. Only widens what is allowed: the
-- previous version writes none but the old values, and reads system as
-- asked for the few seconds both run.
ALTER TABLE ai_settings DROP CONSTRAINT IF EXISTS ai_settings_code_agent_check;
ALTER TABLE ai_settings ADD CONSTRAINT ai_settings_code_agent_check
    CHECK (code_agent IN ('off', 'asked', 'system', 'auto'));
-- The first organization had the agent on for what it is sent; it now also
-- takes what rendimiento opens. Once: a later choice stays.
UPDATE ai_settings SET code_agent = 'system', updated_by = 'rendimiento', updated_at = now()
    WHERE org_id = 1 AND code_agent = 'asked';
-- estimate_learn: the estimator multiplies its estimates by what the coding
-- agent really spends (false: the fixed price table). Only adds.
ALTER TABLE ai_settings ADD COLUMN IF NOT EXISTS estimate_learn BOOLEAN NOT NULL DEFAULT true;
