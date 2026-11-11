-- Merging what a person approved: an app that merges by itself merges only
-- the runs someone approved in rendimiento, unless it asks for any open
-- pull request (merge_any, how it was before). Each run remembers who
-- approved it. Only adds.
ALTER TABLE apps ADD COLUMN IF NOT EXISTS merge_any BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE runs ADD COLUMN IF NOT EXISTS approved_by TEXT NOT NULL DEFAULT '';
