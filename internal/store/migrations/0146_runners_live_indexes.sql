-- A reconcile pass reads the live runners every few seconds, once for the fleet and once
-- for each pool, and the table also keeps every runner that was removed, for the history
-- pages and the usage figures. A fleet that runs thousands of jobs a day makes thousands
-- of runners a day, so the live ones are a few dozen rows among tens of thousands the
-- reads do not ask for: the fleet read walked all of them in creation order and threw
-- most away, and the pool read searched every runner the pool ever had. A partial index
-- over only the live rows, in the order the reads ask for, answers each from a range
-- whose size is the live runners and not the history. The predicate has to match the
-- queries' text, which is why those statements are constants with a plan test.
CREATE INDEX idx_runners_live_created ON runners(created_at) WHERE state != 'removed';
CREATE INDEX idx_runners_live_pool_created ON runners(pool_id, created_at) WHERE state != 'removed';
