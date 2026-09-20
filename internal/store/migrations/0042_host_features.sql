-- What an agent can do beyond running a backend: the features it advertises
-- on every heartbeat, such as moving a live runner's CPU quota. Kept on the
-- row so that a pool being edited and a host's card can say which machines
-- will honour an elastic pool now, rather than the first heartbeat after the
-- edit finding out. JSON like backends, so a new feature is a value and not a
-- column.
ALTER TABLE hosts ADD COLUMN features TEXT NOT NULL DEFAULT '[]';
