-- Repository-wide watching is its own explicit decision. A per-PR watch must
-- never be read as permission to follow every other PR in the repository.
CREATE TABLE eli_repair_watch_repositories (
    id TEXT PRIMARY KEY,
    installation_id TEXT NOT NULL REFERENCES installations(id) ON DELETE CASCADE,
    repo TEXT NOT NULL,
    owner_id TEXT NOT NULL DEFAULT '',
    checks INTEGER NOT NULL DEFAULT 0,
    conflicts INTEGER NOT NULL DEFAULT 0,
    enabled INTEGER NOT NULL DEFAULT 1,
    last_discovered_at INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX eli_repair_watch_repositories_key ON eli_repair_watch_repositories(repo COLLATE NOCASE, installation_id);
CREATE INDEX eli_repair_watch_repositories_due ON eli_repair_watch_repositories(enabled, last_discovered_at);
