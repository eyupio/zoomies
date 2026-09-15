-- The category behind a job's failure, beside the sentence that already
-- described it.
--
-- GitHub records a job whose runner died exactly as it records a job whose
-- tests failed. jobs.runner_fault has told the two apart in prose since it was
-- added, which reads well on one job and counts nothing across a thousand:
-- "fourteen of your failures this week were out of memory" was not a question
-- anything could answer.
--
-- It is its own migration, apart from the same column on runners, because the
-- jobs table is rebuilt by 0009 and a rebuilt database replays every migration
-- that touches it. One that also altered another table could not be replayed:
-- the second ALTER would find its column already there and stop the upgrade.
ALTER TABLE jobs ADD COLUMN fault_kind TEXT NOT NULL DEFAULT '';

-- Every job that already carries a fault is a fleet failure, and leaving these
-- empty would hand them all back to the workflows that did nothing wrong the
-- moment the split started being counted. The kind these rows get is the
-- unclassified one, which is the truth: the message was never categorised, and
-- inventing a category by reading it back would be a guess.
UPDATE jobs SET fault_kind = 'runner_exited' WHERE runner_fault != '';

-- The Overview's split and the problems panel both ask for recent failures by
-- category, which is otherwise a scan of a table that keeps every job the fleet
-- has seen.
CREATE INDEX idx_jobs_fault_kind ON jobs (fault_kind, completed_at);
