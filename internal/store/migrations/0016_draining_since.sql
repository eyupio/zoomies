-- When a runner entered draining, so a drain that never finishes can be caught.
--
-- The agent's stop task lives in an in-memory queue by design, so a controller
-- restart drops it. The row stays in draining, and nothing counts against it:
-- it holds its host slot and its pool sits one runner short for as long as the
-- controller runs. Created-at is the wrong clock -- a runner that worked for a
-- day before being drained is not overdue the moment it drains -- so the
-- moment it entered the state is recorded on the row, where a restart cannot
-- lose it.
ALTER TABLE runners ADD COLUMN draining_since INTEGER;
