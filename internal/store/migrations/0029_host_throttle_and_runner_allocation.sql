-- What the controller decided about a host under pressure, and what each
-- runner was given.
--
-- The throttle is a decision rather than a measurement, which is why it is
-- not a field of the usage column beside it: usage is what the agent said,
-- rewritten on every heartbeat, and the throttle has to outlast any one
-- sample. The controller alone writes it.
ALTER TABLE hosts ADD COLUMN throttle TEXT NOT NULL DEFAULT '{}';
-- A runner's limits are recorded when its workload is created, because the
-- question they answer is about the past: a runner killed for exceeding its
-- memory limit has to say which limit, and a pool edited since, or a host
-- whose capacity has moved, would give a different answer today. The source
-- says whether the pool set them or the host's default share did, which is
-- the difference between "raise memory_mb" and "lower the host's capacity".
ALTER TABLE runners ADD COLUMN allocated_cpus REAL NOT NULL DEFAULT 0;
ALTER TABLE runners ADD COLUMN allocated_memory_mb INTEGER NOT NULL DEFAULT 0;
ALTER TABLE runners ADD COLUMN allocation_source TEXT NOT NULL DEFAULT '';
