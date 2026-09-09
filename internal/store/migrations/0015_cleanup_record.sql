-- What happened when Zoomies tried to take a runner away.
--
-- Cleanup failure had nowhere to live. A stop or remove that the agent could
-- not complete came back as a task result for a row that was already terminal,
-- and the state machine refused the transition and dropped it at debug level:
-- correct, in that a removed runner cannot become failed, and useless, in that
-- the container is still on the host and nobody is told. A failed registration
-- delete was a log line and a counter. Neither reached the Runners page, the
-- drawer, or a problem code, so the operator's first sign of either was a host
-- filling up or an organisation's runner list filling with ghosts.
--
-- These columns are that record. They sit on the runner row rather than in an
-- operations table because the runner is what the failure is about, and the
-- row is what an operator already looks at.
ALTER TABLE runners ADD COLUMN cleanup_error TEXT NOT NULL DEFAULT '';
ALTER TABLE runners ADD COLUMN cleanup_failed_at INTEGER;
ALTER TABLE runners ADD COLUMN cleanup_attempts INTEGER NOT NULL DEFAULT 0;

-- registration_deleted_at is when GitHub confirmed the registration was gone.
-- Unset on a terminal runner is the ghost: a row Zoomies has finished with, and
-- a registration still on somebody's organisation.
ALTER TABLE runners ADD COLUMN registration_deleted_at INTEGER;

-- cleaned_up_at is the end of the cleanup interval the measurement contract
-- asks for: the moment nothing of this runner is left, on the host or on
-- GitHub. It is the counterpart of created_at at the other end of the life.
ALTER TABLE runners ADD COLUMN cleaned_up_at INTEGER;
