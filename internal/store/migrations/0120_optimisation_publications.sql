-- A publication is the draft pull request opened for one approved retained
-- preview. It is separate from the preview and its approval, cascades with the
-- preview, and records exactly what was written where. Nothing here is a merge.
CREATE TABLE optimisation_publications (
    preview_id      TEXT PRIMARY KEY REFERENCES optimisation_previews(id) ON DELETE CASCADE,
    installation_id TEXT NOT NULL,
    repository      TEXT NOT NULL,
    published_by    TEXT NOT NULL,
    branch          TEXT NOT NULL,
    base_branch     TEXT NOT NULL,
    base_sha        TEXT NOT NULL,
    head_sha        TEXT NOT NULL,
    source_sha256   TEXT NOT NULL,
    preview_sha256  TEXT NOT NULL,
    pull_number     INTEGER NOT NULL CHECK(pull_number > 0),
    pull_url        TEXT NOT NULL,
    created_at      INTEGER NOT NULL
);
CREATE INDEX optimisation_publication_scope
    ON optimisation_publications(installation_id, repository, preview_id);
