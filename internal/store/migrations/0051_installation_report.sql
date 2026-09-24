-- What the per-installation report needs to outlive the rows it is read from.
--
-- The report counts, per installation and per day, the jobs this fleet saw
-- and what became of them, and how the runners it started were cleaned up.
-- Job rows are pruned after retention.jobs -- thirty days by default -- and
-- runner rows after retention.runners, so a month's report read from the rows
-- alone is short by whatever the prune has already taken. installation_daily
-- is that report's roll-up, written by the prune loop before the rows go, the
-- same way usage_daily is written for runner allocation.
--
-- Counts only: a percentile cannot be summed across days, and a roll-up of
-- p95s would be a number nobody could defend. The timings are read from
-- runner_sessions, which are kept a year, and the report says where they stop
-- rather than approximate them from anything coarser.
CREATE TABLE installation_daily (
    day               INTEGER NOT NULL,
    installation_id   TEXT    NOT NULL,
    observed          INTEGER NOT NULL DEFAULT 0,
    eligible          INTEGER NOT NULL DEFAULT 0,
    created_for       INTEGER NOT NULL DEFAULT 0,
    ran_here          INTEGER NOT NULL DEFAULT 0,
    ran_elsewhere     INTEGER NOT NULL DEFAULT 0,
    fleet_fault       INTEGER NOT NULL DEFAULT 0,
    cleanup_pending   INTEGER NOT NULL DEFAULT 0,
    cleanup_converged INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (day, installation_id)
);

-- [rolled_from, rolled_until): rolled_from is the exact instant the oldest row
-- the first roll-up found, not its day, because the rows before it on that day
-- were already gone and the report must not claim the whole day.
CREATE TABLE installation_rollup (
    id           INTEGER PRIMARY KEY CHECK (id = 1),
    rolled_from  INTEGER NOT NULL,
    rolled_until INTEGER NOT NULL
);

-- The two ends of the scheduling and registration intervals, carried on the
-- session so the timings survive the runner and job rows. Sessions written
-- before this file have them only where the runner row is still here to copy
-- from; the rest are missing intervals, which the report excludes rather than
-- guesses.
ALTER TABLE runner_sessions ADD COLUMN create_task_issued_at INTEGER;
ALTER TABLE runner_sessions ADD COLUMN job_eligible_at INTEGER;

UPDATE runner_sessions SET
    create_task_issued_at = (SELECT r.create_task_issued_at FROM runners r WHERE r.id = runner_sessions.runner_id),
    job_eligible_at = (SELECT j.eligible_at FROM jobs j WHERE j.id = runner_sessions.job_id);

CREATE INDEX idx_runner_sessions_job ON runner_sessions(job_id);
