-- A durable watch is admitted explicitly by a person and survives a controller
-- restart. It is not the repair ledger: an attempt may point at the watch that
-- authorised it, and an existing one-attempt policy has no watch at all.
CREATE TABLE eli_repair_watches (
    id TEXT PRIMARY KEY,
    installation_id TEXT NOT NULL REFERENCES installations(id) ON DELETE CASCADE,
    repo TEXT NOT NULL,
    pull_number INTEGER NOT NULL,
    owner_id TEXT NOT NULL DEFAULT '',
    policy_provider_id TEXT NOT NULL DEFAULT '',
    policy_updated_at INTEGER NOT NULL DEFAULT 0,
    checks INTEGER NOT NULL DEFAULT 0,
    conflicts INTEGER NOT NULL DEFAULT 0,
    enabled INTEGER NOT NULL DEFAULT 1,
    paused_reason TEXT NOT NULL DEFAULT '',
    head_sha TEXT NOT NULL DEFAULT '',
    base_ref TEXT NOT NULL DEFAULT '',
    base_sha TEXT NOT NULL DEFAULT '',
    last_observed_at INTEGER NOT NULL DEFAULT 0,
    next_check_at INTEGER NOT NULL DEFAULT 0,
    lease_token TEXT NOT NULL DEFAULT '',
    lease_until INTEGER NOT NULL DEFAULT 0,
    stop_reason TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
-- GitHub sends repository names with inconsistent casing, and a person may
-- select the same pull request from either spelling.
CREATE UNIQUE INDEX eli_repair_watches_pull ON eli_repair_watches(repo COLLATE NOCASE, pull_number);
CREATE INDEX eli_repair_watches_due ON eli_repair_watches(enabled, next_check_at);
-- An attempt records the watch that authorised it, so a later watcher can tell
-- an explicit continuous repair from the legacy one-attempt automatic mode.
ALTER TABLE eli_repairs ADD COLUMN watch_id TEXT NOT NULL DEFAULT '';
CREATE INDEX eli_repairs_watch ON eli_repairs(watch_id) WHERE watch_id<>'';
