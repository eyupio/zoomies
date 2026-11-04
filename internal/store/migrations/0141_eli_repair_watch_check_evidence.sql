-- Keep the bounded producers and outcomes behind a watched check state, so the
-- page can explain which checks failed instead of showing one opaque result.
ALTER TABLE eli_repair_watches ADD COLUMN check_evidence TEXT NOT NULL DEFAULT '[]';
