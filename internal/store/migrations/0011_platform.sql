-- Platform: what a pool's runners are, and what a host actually is.
--
-- Before this, a pool said which backend it wanted and nothing about the
-- machine underneath, so the scheduler could put a Debian-image runner on a
-- macOS host and only the job's failure would say so. These columns are what
-- the placement rule and the pool's default image both read.
--
-- Every column is nullable-by-default-empty, so an existing fleet keeps
-- working unchanged: an empty platform means "makes no promise", which is
-- exactly the behaviour those pools have today.

ALTER TABLE pools ADD COLUMN os         TEXT NOT NULL DEFAULT '';
ALTER TABLE pools ADD COLUMN os_version TEXT NOT NULL DEFAULT '';
ALTER TABLE pools ADD COLUMN arch       TEXT NOT NULL DEFAULT '';

-- Hosts already reported os (the kernel) and arch. distro and os_version are
-- what tell an Ubuntu 22.04 machine from a Debian 12 one -- "linux" cannot
-- pick a runner image -- and cpus/memory are what make a host's name and the
-- Hosts page say how much machine it is.
ALTER TABLE hosts ADD COLUMN distro     TEXT    NOT NULL DEFAULT '';
ALTER TABLE hosts ADD COLUMN os_version TEXT    NOT NULL DEFAULT '';
ALTER TABLE hosts ADD COLUMN cpus       INTEGER NOT NULL DEFAULT 0;
ALTER TABLE hosts ADD COLUMN memory_mb  INTEGER NOT NULL DEFAULT 0;
