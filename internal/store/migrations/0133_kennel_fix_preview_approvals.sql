-- An approval of a kept Kennel fix preview is a separate, attributed act.
-- It binds the preview's head, workflow digest and preview hash, expires with the
-- preview through the cascade, and publishes nothing.
CREATE TABLE kennel_fix_preview_approvals (
    preview_id      TEXT PRIMARY KEY REFERENCES kennel_fix_previews(id) ON DELETE CASCADE,
    installation_id TEXT NOT NULL,
    repository      TEXT NOT NULL,
    approved_by     TEXT NOT NULL,
    head_sha        TEXT NOT NULL,
    source_sha256   TEXT NOT NULL,
    preview_sha256  TEXT NOT NULL,
    created_at      INTEGER NOT NULL
);
CREATE INDEX kennel_fix_preview_approval_scope
    ON kennel_fix_preview_approvals(installation_id, repository, preview_id);
