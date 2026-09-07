-- The Overview's sparklines counted every job GitHub reported, hosted runners
-- included, so on an organisation that also uses hosted runners the queue depth
-- behind the tiles was somebody else's. The two new columns record the same
-- minute narrowed to the jobs this fleet has a hand in, so the toggle on the
-- page can move the line without waiting an hour for new history.
--
-- Existing rows keep 0 rather than being backfilled: a sample is a measurement
-- taken at a minute that has passed, and there is no honest way to work out now
-- what the fleet's own queue was then.
ALTER TABLE fleet_samples ADD COLUMN fleet_queued_jobs  INTEGER NOT NULL DEFAULT 0;
ALTER TABLE fleet_samples ADD COLUMN fleet_running_jobs INTEGER NOT NULL DEFAULT 0;
