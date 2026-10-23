-- One retained comparison may be linked to a published optimisation as its
-- verification evidence. It is a separate record and never rewrites the
-- comparison or the publication; the preview expires with both.
CREATE TABLE optimisation_publication_verifications (
    preview_id      TEXT PRIMARY KEY REFERENCES optimisation_publications(preview_id) ON DELETE CASCADE,
    comparison_id   TEXT NOT NULL,
    installation_id TEXT NOT NULL,
    repository      TEXT NOT NULL,
    linked_by       TEXT NOT NULL,
    created_at      INTEGER NOT NULL
);
CREATE INDEX optimisation_publication_verification_scope
    ON optimisation_publication_verifications(installation_id, repository, preview_id);
