-- The attention list drops a failure once its branch has moved to another
-- commit, and an attempt of a Dependabot update once the update has run again.
-- Both ask for the runs that came after this one on its branch, and neither
-- names the workflow, so idx_jobs_attention_branch stops helping after the
-- branch: on a default branch that is every job the repository has ever run
-- there, read once for each failure in the list. This index ends on the run id
-- the question is asked in, and carries the commit so the first of the two is
-- answered without reading a row.
CREATE INDEX idx_jobs_branch_run ON jobs(repo, head_branch, github_run_id, head_sha);
