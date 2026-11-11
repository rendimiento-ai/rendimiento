-- Merging by itself: an app may ask rendimiento to merge a branch's open
-- pull request once the branch's run passes. Each run remembers the pull
-- request it was about and what became of it (merged, conflict, blocked).
-- Only adds.
ALTER TABLE apps ADD COLUMN IF NOT EXISTS auto_merge BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE runs ADD COLUMN IF NOT EXISTS pull_number INTEGER NOT NULL DEFAULT 0;
ALTER TABLE runs ADD COLUMN IF NOT EXISTS merge_state TEXT NOT NULL DEFAULT ''
    CHECK (merge_state IN ('', 'merged', 'conflict', 'blocked'));
