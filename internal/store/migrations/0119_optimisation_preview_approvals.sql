-- An approval is a separate, attributed act from keeping a preview. It binds
-- one exact retained preview at the head and source digest the operator was
-- shown, and expires with that preview: deleting or pruning the preview
-- cascades here, and reads require the preview to still be within its lifetime.
CREATE TABLE optimisation_preview_approvals (
    preview_id     TEXT PRIMARY KEY REFERENCES optimisation_previews(id) ON DELETE CASCADE,
    installation_id TEXT NOT NULL,
    repository     TEXT NOT NULL,
    approved_by    TEXT NOT NULL,
    head_sha       TEXT NOT NULL,
    source_sha256  TEXT NOT NULL,
    preview_sha256 TEXT NOT NULL,
    created_at     INTEGER NOT NULL
);
CREATE INDEX optimisation_preview_approval_scope
    ON optimisation_preview_approvals(installation_id, repository, preview_id);
