-- A publication is the one draft pull request opened for an approved
-- Kennel fix preview. It cascades with the preview, names the branch and base it
-- used, and records exactly what was written where. Nothing here is a merge.
CREATE TABLE kennel_fix_publications (
    preview_id      TEXT PRIMARY KEY REFERENCES kennel_fix_previews(id) ON DELETE CASCADE,
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
CREATE INDEX kennel_fix_publication_scope
    ON kennel_fix_publications(installation_id, repository, preview_id);
