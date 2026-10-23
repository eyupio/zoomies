-- Private source-bearing cache previews have their own bounded,
-- installation-scoped history. The lockfile content is not retained, only its
-- path, mode and SHA-256, so a large lockfile stays out of the database.
CREATE TABLE optimisation_cache_previews (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT,
    id TEXT NOT NULL UNIQUE,
    installation_id TEXT NOT NULL REFERENCES installations(id) ON DELETE CASCADE,
    repository TEXT NOT NULL,
    initiating_user TEXT NOT NULL,
    version TEXT NOT NULL,
    sha256 TEXT NOT NULL,
    payload BLOB NOT NULL CHECK(length(payload) > 0 AND length(payload) <= 262144),
    created_at INTEGER NOT NULL
);
CREATE INDEX optimisation_cache_preview_scope ON optimisation_cache_previews(installation_id, repository, id);
