-- One row per runner that has gone, written once and never updated.
--
-- The usage report used to be computed from runners rows alone, and those are
-- pruned after retention.runners -- seven days by default, and a key the
-- fleet can shorten. A month's runner-hours, the figure an operator reconciles
-- against their cloud bill, was therefore short by everything older than a
-- week. This table is the record that outlives the row: who the runner was
-- for, where it ran, the job it ran if any, when it lived, and what the pool
-- said an hour of it cost at the time, so a later change to the rate does not
-- reprice last month.
--
-- A session is written when the runner's cleanup is confirmed (cleaned_up_at
-- in its 0023 sense: host removal and GitHub's absence both seen), in the same
-- transaction that stamps it. A runner pruned before that ever happened gets
-- its session as the row goes, with cleaned_up_at left NULL, because a ledger
-- that skipped the runners whose cleanup went wrong would be short by exactly
-- the ones an operator is most likely to ask about.
--
-- runner_id is the primary key and every write is INSERT ... ON CONFLICT DO
-- NOTHING, so a confirmation that is retried, or replayed after a controller
-- restart mid-cleanup, cannot write a second session. There are deliberately
-- no foreign keys: the point of the row is to outlive the runner, its host and
-- even its pool.
CREATE TABLE runner_sessions (
    runner_id            TEXT PRIMARY KEY,
    pool_id              TEXT    NOT NULL,
    host_id              TEXT    NOT NULL DEFAULT '',
    installation_id      TEXT    NOT NULL DEFAULT '',
    job_id               TEXT    NOT NULL DEFAULT '',
    started_at           INTEGER NOT NULL,
    registered_at        INTEGER,
    finished_at          INTEGER NOT NULL,
    cleaned_up_at        INTEGER,
    cost_per_runner_hour REAL,
    recorded_at          INTEGER NOT NULL
);
CREATE INDEX idx_runner_sessions_finished ON runner_sessions(finished_at);
CREATE INDEX idx_runner_sessions_started ON runner_sessions(started_at);

-- The runners already cleaned up when this build arrives are recorded now, so
-- the ledger starts from the oldest row the database still has rather than
-- from the upgrade. This reads runners; it does not rewrite them.
INSERT INTO runner_sessions (runner_id, pool_id, host_id, installation_id, job_id,
    started_at, registered_at, finished_at, cleaned_up_at, cost_per_runner_hour, recorded_at)
SELECT r.id, r.pool_id, COALESCE(r.host_id, ''), COALESCE(p.installation_id, ''),
    COALESCE(NULLIF(r.current_job_id, ''),
        (SELECT j.id FROM jobs j WHERE j.runner_id = r.id ORDER BY j.queued_at DESC LIMIT 1), ''),
    r.created_at, r.registered_at, COALESCE(r.finished_at, r.cleaned_up_at), r.cleaned_up_at,
    p.cost_per_runner_hour, r.cleaned_up_at
FROM runners r LEFT JOIN pools p ON p.id = r.pool_id
WHERE r.cleaned_up_at IS NOT NULL;
