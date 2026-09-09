-- Two facts a restart used to lose.
--
-- task_issued_at is when this runner's lifecycle task was last handed to its
-- host. The task queue is in memory by design -- every task is derived from
-- state the database already holds, so persisting the queue would add a second
-- source of truth that could disagree with this table. What the queue is not
-- allowed to lose is the *time*: after a controller restart a provisioning row
-- is indistinguishable from one whose create was handed out a second ago, and
-- the provision timeout is counted from the wrong end. The stamp is written at
-- enqueue and again on every redelivery, so the row answers "how long has this
-- runner been waiting on a task?" without the queue existing.
ALTER TABLE runners ADD COLUMN task_issued_at INTEGER;

-- The agent session columns detect a duplicated agent credential: a cloned VM,
-- or a copied state directory, where two agents share one host id and split
-- its tasks between them.
--
-- An agent mints a session id when it starts, so the id is fresh on every
-- restart and never reused. That is what makes alternation the signal rather
-- than change: a restart moves the id forward once and it stays there, while
-- two agents sharing an identity keep handing it back and forth. Seeing an id
-- this host has already moved on from is therefore something a single agent
-- cannot do, however often it restarts.
--
-- Recorded, not enforced. Refusing the older session would be a coin toss over
-- which of two live agents keeps the host, and both are running real work.
ALTER TABLE hosts ADD COLUMN agent_session_id TEXT NOT NULL DEFAULT '';
ALTER TABLE hosts ADD COLUMN agent_session_prev TEXT NOT NULL DEFAULT '';
ALTER TABLE hosts ADD COLUMN agent_session_alternations INTEGER NOT NULL DEFAULT 0;
ALTER TABLE hosts ADD COLUMN agent_session_alt_at INTEGER;
