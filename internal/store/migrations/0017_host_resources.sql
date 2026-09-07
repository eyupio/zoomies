-- What a host has, and what its operator has set aside.
--
-- Two different kinds of number live here, which is why they are separate
-- columns rather than one. The disk figures are observations: the agent
-- measures the filesystem its work directory sits on and reports them, and a
-- host resized or filled up says so on its next heartbeat. The reserves are
-- the operator's, like capacity is today -- what to hold back from placement
-- for the machine's own sake -- and nothing an agent sends may write them.
ALTER TABLE hosts ADD COLUMN disk_total_mb INTEGER NOT NULL DEFAULT 0;
ALTER TABLE hosts ADD COLUMN disk_free_mb INTEGER NOT NULL DEFAULT 0;
ALTER TABLE hosts ADD COLUMN reserve_cpus INTEGER NOT NULL DEFAULT 0;
ALTER TABLE hosts ADD COLUMN reserve_memory_mb INTEGER NOT NULL DEFAULT 0;
ALTER TABLE hosts ADD COLUMN reserve_disk_mb INTEGER NOT NULL DEFAULT 0;
