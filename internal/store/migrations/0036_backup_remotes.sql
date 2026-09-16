-- The offsite destinations an administrator adds from the Backups page.
--
-- `backup.remotes` in zoomies.yaml already describes destinations, and it
-- stays: it is the one that can be read on a host whose database is gone,
-- which is the day an offsite copy is for. This table is the other half --
-- the destinations somebody adds, tests and edits in the UI, without a file
-- to open or a controller to restart.
--
-- The two secrets are sealed with the instance key exactly as a provider's
-- credential is, and for the same reason: a row that can be read back over
-- the API, copied into a bug report or lifted out of a backup must not carry
-- the key to somebody else's bucket. The access key id is not sealed -- it
-- is an identifier, it appears in the service's own access logs, and an
-- operator comparing two destinations needs to see it.
--
-- The name is unique because it is how every other surface addresses a
-- destination: the API route, the log line, the problems drawer entry. A
-- configuration file that names the same destination wins over this row, on
-- the principle the rest of the configuration follows -- the file and the
-- environment have the last word -- and the page says so rather than showing
-- two rows that disagree.
CREATE TABLE backup_remotes (
    id             TEXT PRIMARY KEY,
    name           TEXT    NOT NULL UNIQUE,
    endpoint       TEXT    NOT NULL,
    region         TEXT    NOT NULL DEFAULT '',
    bucket         TEXT    NOT NULL,
    prefix         TEXT    NOT NULL DEFAULT '',
    access_key_id  TEXT    NOT NULL DEFAULT '',
    secret_key_enc BLOB,                          -- AES-256-GCM under the instance key
    passphrase_enc BLOB,                          -- the same; empty means the archive goes as it is
    -- NULL asks for the style to be chosen from the endpoint: virtual-hosted
    -- for AWS's own, path style for everything else, which is what MinIO,
    -- Ceph and a bare address need.
    path_style     INTEGER,
    keep           INTEGER NOT NULL DEFAULT 0,    -- 0 keeps every copy, as backup.keep does
    enabled        INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
    created_at     INTEGER NOT NULL,
    updated_at     INTEGER NOT NULL
);
