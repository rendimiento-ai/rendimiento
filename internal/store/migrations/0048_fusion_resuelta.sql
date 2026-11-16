-- A run's pull request may also be "resolved": it conflicted only where
-- both sides added lines, and rendimiento joined them. The check of 0036
-- did not know that state, so recording it failed. Only widens what is
-- allowed: the previous version writes none but the old values.
ALTER TABLE runs DROP CONSTRAINT IF EXISTS runs_merge_state_check;
ALTER TABLE runs ADD CONSTRAINT runs_merge_state_check
    CHECK (merge_state IN ('', 'merged', 'conflict', 'resolved', 'blocked'));
