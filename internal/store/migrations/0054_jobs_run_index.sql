-- A run's jobs are found by (github_run_id, repo): the webhook that saves a
-- run number onto every job of the run, and the lookup that finds one already
-- recorded, both do it on every job event. With no index each was a scan of
-- the whole jobs table. 0053 now creates this before its backfill; a database
-- that applied 0053 before it did gets the index here.
CREATE INDEX IF NOT EXISTS idx_jobs_run ON jobs(github_run_id, repo);
