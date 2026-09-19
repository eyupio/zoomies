-- An operator's cancellation reaches GitHub immediately, but GitHub's own
-- completion delivery is what settles the job's conclusion, and it can be
-- seconds or minutes behind. Until then the row still said `queued` or
-- `in_progress`, so the fleet went on reporting cancelled work as waiting or
-- running -- a queue depth nobody could clear, and a Jobs page listing work
-- that had already been called off.
--
-- This is the fleet's own note that the request was made and accepted. It is
-- never written by a delivery, so a webhook replay cannot lose it, and the
-- conclusion is still GitHub's to give.
ALTER TABLE jobs ADD COLUMN cancel_requested_at INTEGER;

-- The queue and the running list are both read by "is this job still the
-- fleet's to do?", so the index covers the states this narrows.
CREATE INDEX idx_jobs_cancel_requested ON jobs(state, cancel_requested_at);
