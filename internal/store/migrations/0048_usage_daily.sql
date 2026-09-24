-- A day's runner allocation per pool, host and installation, rolled up from
-- runner_sessions by the prune loop before anything it is computed from goes.
--
-- The sessions are kept a year, and a key can shorten that; the roll-up is
-- what keeps a report complete for every day it covers whatever either
-- retention is set to. It is written in integers -- whole seconds, and cost in
-- hundredths of the pool's currency -- so a month summed from thirty days is
-- exactly the month, rather than thirty rounding errors apart from it.
--
-- day is the UTC midnight the row covers, in milliseconds like every other
-- instant in this schema. cost_minor is NULL, not 0, when no session that day
-- had a rate: "not priced" and "free" are different answers.
CREATE TABLE usage_daily (
    day               INTEGER NOT NULL,
    pool_id           TEXT    NOT NULL,
    host_id           TEXT    NOT NULL,
    installation_id   TEXT    NOT NULL,
    allocated_seconds INTEGER NOT NULL,
    cost_minor        INTEGER,
    PRIMARY KEY (day, pool_id, host_id, installation_id)
);

-- One row saying which days the roll-up has absorbed: [rolled_from,
-- rolled_until). The report reads usage_daily below rolled_until and the rows
-- above it, and a day is never rolled up twice, because the watermark moves in
-- the transaction that writes the day.
CREATE TABLE usage_rollup (
    id           INTEGER PRIMARY KEY CHECK (id = 1),
    rolled_from  INTEGER NOT NULL,
    rolled_until INTEGER NOT NULL
);
