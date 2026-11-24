-- Why the coding agent gave an encargo back, as a short code recorded when
-- it lets it go (store.Cause*: budget, needs_decision, no_tools, ci_timeout,
-- ai_error, forge_error, conflict, stopped, restart, other), for the AI
-- page's «How the coding agent is doing». NULL on every other entry, and on
-- the releases from before it: those are classified by their note when
-- read (store.ClassifyCause), approximately.
-- Only adds a nullable column: the previous version neither writes nor
-- reads it.
ALTER TABLE encargo_log ADD COLUMN IF NOT EXISTS cause TEXT;
