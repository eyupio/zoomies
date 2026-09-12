-- Recover unfinished host removals without repeatedly scanning runner history
-- or all jobs on busy fleets.
CREATE INDEX idx_runners_pending_host_cleanup ON runners(id)
    WHERE state IN ('removed', 'failed') AND host_removed_at IS NULL;
CREATE INDEX idx_jobs_runner_state ON jobs(runner_id, state);
