-- GitHub's own sequential run number ("#1009" in its Actions UI), backfilled
-- from a workflow_run lookup because the workflow_job webhook never carries
-- it. Zero means that lookup has not happened yet.
ALTER TABLE jobs ADD COLUMN run_number INTEGER NOT NULL DEFAULT 0;
