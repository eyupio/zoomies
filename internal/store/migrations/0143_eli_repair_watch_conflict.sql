-- Keep the pinned target tip, merge base and GitHub's mergeability answer with
-- the watched head, so a later conflict repair can detect movement and refuse a
-- guessed conflict.
ALTER TABLE eli_repair_watches ADD COLUMN conflict_evidence TEXT NOT NULL DEFAULT '{}';
