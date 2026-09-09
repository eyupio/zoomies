-- A lifecycle retry must not overwrite the first create delivered to a host.
ALTER TABLE runners ADD COLUMN create_task_issued_at INTEGER;
ALTER TABLE runners ADD COLUMN host_removed_at INTEGER;

-- Earlier cleanup stamps represented the first success signal, not confirmed
-- removal on both sides. Keep them as historical estimates, never as proof.
ALTER TABLE runners ADD COLUMN cleanup_estimated_at INTEGER;
UPDATE runners SET cleanup_estimated_at=cleaned_up_at, cleaned_up_at=NULL;

-- A deployment review still held by GitHub is not eligible for placement.
UPDATE jobs SET eligible_at=NULL WHERE state='waiting';
