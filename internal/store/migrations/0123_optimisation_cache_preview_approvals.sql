-- An approval of a kept setup cache preview is a separate, attributed act.
-- It binds the preview's head, workflow digest, lockfile digest and preview
-- hash, expires with the preview through the cascade, and publishes nothing.
CREATE TABLE optimisation_cache_preview_approvals (
    preview_id      TEXT PRIMARY KEY REFERENCES optimisation_cache_previews(id) ON DELETE CASCADE,
    installation_id TEXT NOT NULL,
    repository      TEXT NOT NULL,
    approved_by     TEXT NOT NULL,
    head_sha        TEXT NOT NULL,
    source_sha256   TEXT NOT NULL,
    preview_sha256  TEXT NOT NULL,
    created_at      INTEGER NOT NULL
);
CREATE INDEX optimisation_cache_preview_approval_scope
    ON optimisation_cache_preview_approvals(installation_id, repository, preview_id);
