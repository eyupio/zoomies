-- The same category on runners, which is the half no job ever sees.
--
-- A runner that fails before it registers never reaches a job at all: the job
-- stays queued, waits for the next runner, and waits again. From the Jobs page
-- a pool whose containers will not start was indistinguishable from a pool that
-- was merely busy, and "unable to start the runner container" -- the commonest
-- way a self-hosted fleet breaks -- was a sentence on one runner's page and a
-- number nowhere.
--
-- Rows already failed are left unclassified rather than guessed at from their
-- message. The counts start from here.
ALTER TABLE runners ADD COLUMN fault_kind TEXT NOT NULL DEFAULT '';

-- The problems panel asks for a pool's recent failed starts on every pass.
CREATE INDEX idx_runners_fault_kind ON runners (fault_kind, finished_at);
