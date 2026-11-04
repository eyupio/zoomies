-- The watcher records what the last read of a watched head said, so a later
-- repair chain can tell a fresh failure from an unknown or incomplete read.
ALTER TABLE eli_repair_watches ADD COLUMN check_state TEXT NOT NULL DEFAULT 'unknown';
