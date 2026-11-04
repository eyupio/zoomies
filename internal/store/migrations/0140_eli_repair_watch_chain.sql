-- A watch keeps the number and last shape of the repairs it has started, so a
-- restart or a redelivered check cannot reset the chain back to a free attempt.
ALTER TABLE eli_repair_watches ADD COLUMN chain_attempts INTEGER NOT NULL DEFAULT 0;
ALTER TABLE eli_repair_watches ADD COLUMN last_failure_signature TEXT NOT NULL DEFAULT '';
ALTER TABLE eli_repair_watches ADD COLUMN chain_stopped_reason TEXT NOT NULL DEFAULT '';
