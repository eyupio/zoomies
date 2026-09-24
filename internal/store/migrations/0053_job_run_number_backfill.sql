-- A job's run number was only ever saved on the first job of its run: every
-- sibling found the number already recorded, used it for that delivery and
-- never wrote it down, so the Runners and Jobs pages showed "#1114" beside
-- one job of a run and nothing beside the rest. Copy it from a sibling that
-- has it. Idempotent, touches only rows still at 0, and a controller rolled
-- back past this release reads the same column it always did.
UPDATE jobs SET run_number = (
    SELECT s.run_number FROM jobs s
    WHERE s.github_run_id = jobs.github_run_id AND s.repo = jobs.repo AND s.run_number != 0
    LIMIT 1
)
WHERE run_number = 0 AND github_run_id != 0 AND EXISTS (
    SELECT 1 FROM jobs s
    WHERE s.github_run_id = jobs.github_run_id AND s.repo = jobs.repo AND s.run_number != 0
);
