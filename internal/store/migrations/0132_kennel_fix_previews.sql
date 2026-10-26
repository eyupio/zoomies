-- One retained Kennel fix preview: the exact workflow source the finding names,
-- the operator's value and the computed edit. It is private, bounded and
-- separate from the optimisation histories.
CREATE TABLE kennel_fix_previews (
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
CREATE INDEX kennel_fix_preview_scope
    ON kennel_fix_previews(installation_id, repository, id);
CREATE INDEX kennel_fix_preview_created
    ON kennel_fix_previews(created_at);
