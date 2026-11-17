-- A release by the run that made it: the work board and the runs' history
-- ask, for each run, for its release (RunHistory). Without this index each
-- run read the whole table: about 1 s for an app with 450 runs in two
-- weeks. Only adds: the previous version reads the same table, faster.
CREATE INDEX IF NOT EXISTS releases_run ON releases (run_id);
