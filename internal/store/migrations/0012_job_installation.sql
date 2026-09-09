-- Which GitHub App installation a job belongs to.
--
-- A job row carried no installation identity, so a pool was chosen for it on
-- labels alone. Two installations whose pools advertise the same labels then
-- cross-allocated deterministically -- the tie-break is the pool name -- and a
-- runner was minted in the wrong GitHub target. This column is the other half
-- of the comparison the eligibility rule now makes against pools.installation_id.
--
-- It carries no foreign key, like pool_id and runner_id beside it. A finished
-- job is history, and removing an installation must not delete the record of
-- what it ran.
ALTER TABLE jobs ADD COLUMN installation_id TEXT NOT NULL DEFAULT '';

-- The same identity on a delivery. Verification deliberately falls back to any
-- configured secret when no installation covers the repository, so "which
-- installation signed this" and "which installation owns this work" are
-- different questions; the delivery log could previously answer neither.
ALTER TABLE webhook_deliveries ADD COLUMN installation_id TEXT NOT NULL DEFAULT '';

-- Attribute the work that has not finished yet. A completed job is history and
-- is left alone; a waiting, queued or in-progress one is still a scheduling
-- decision, and it has to be made against the right target. The precedence is
-- the one FindInstallationByTarget uses: an installation on the repository
-- itself before one on the organisation that owns it. A repository no
-- installation covers keeps the empty string, which the eligibility rule
-- reports as a reason rather than treating as a wildcard.
UPDATE jobs
   SET installation_id = COALESCE((
           SELECT i.id FROM installations i
            WHERE (i.target_type = 'repo' AND i.target = jobs.repo)
               OR (i.target_type = 'org'  AND i.target = CASE
                       WHEN instr(jobs.repo, '/') > 1
                       THEN substr(jobs.repo, 1, instr(jobs.repo, '/') - 1)
                       ELSE jobs.repo END)
            ORDER BY CASE i.target_type WHEN 'repo' THEN 0 ELSE 1 END
            LIMIT 1
       ), '')
 WHERE state IN ('waiting', 'queued', 'in_progress');

-- Undo a match this change has just made wrong. A queued job claimed under the
-- old label-only rule may sit on a pool belonging to another installation, and
-- that pool will never run it; the match is sticky through the upsert, so no
-- later delivery can correct it. Clearing both lets the next pass decide again
-- and the Jobs page say why nothing claims the job.
--
-- An in-progress job is left exactly as it is. Its runner already exists, and
-- rewriting the row would lose the record of where the job actually ran --
-- which, on a fleet that has been cross-allocating, is the evidence.
--
-- A job whose repository no installation covers is unclaimed by the same rule,
-- because no pool's installation can equal its empty one. That is deliberate
-- and not merely a consequence: such a job cannot be run here at all, and a
-- pool named beside it on the Jobs page would be a promise nothing can keep.
--
-- On a controller with one installation this statement matches nothing: every
-- pool belongs to that installation, and every attributed job now does too.
UPDATE jobs
   SET matched = 0, pool_id = ''
 WHERE state IN ('waiting', 'queued')
   AND pool_id <> ''
   AND pool_id IN (SELECT p.id FROM pools p WHERE p.installation_id <> jobs.installation_id);
