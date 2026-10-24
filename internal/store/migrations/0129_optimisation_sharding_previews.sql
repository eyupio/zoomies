-- One retained Playwright sharding preview: the exact workflow source, the
-- timing samples and the computed edit. It is private, bounded and separate
-- from the checkout, cache and readiness histories.
CREATE TABLE optimisation_sharding_previews (
    sequence        INTEGER PRIMARY KEY AUTOINCREMENT,
    id              TEXT NOT NULL UNIQUE,
    installation_id TEXT NOT NULL REFERENCES installations(id) ON DELETE CASCADE,
    repository      TEXT NOT NULL,
    initiating_user TEXT NOT NULL,
    version         TEXT NOT NULL,
    sha256          TEXT NOT NULL,
    payload         BLOB NOT NULL,
    created_at      INTEGER NOT NULL
);
CREATE INDEX optimisation_sharding_preview_scope
    ON optimisation_sharding_previews(installation_id, repository, id);
CREATE INDEX optimisation_sharding_preview_created
    ON optimisation_sharding_previews(created_at);
