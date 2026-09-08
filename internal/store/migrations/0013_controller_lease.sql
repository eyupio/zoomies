-- One controller at a time over one database.
--
-- SQLite's busy timeout serialises writes; it does not stop a second
-- controller from running. Two schedulers over one database both mint runner
-- credentials, both reclaim hosts they think are lost and both reap workloads
-- the other created, and every symptom of it looks like a bug somewhere else.
--
-- The lease is the half that works across machines: a file lock beside the
-- database catches a second process on the same host, but a restored copy on a
-- shared filesystem, or a controller on another host pointed at the same file,
-- is caught only by a row the second one has to read.
--
-- One row, enforced by the primary key: there is one control plane per
-- database, so a table that could hold two of them would be modelling
-- something this design does not have.
CREATE TABLE controller_lease (
    id          INTEGER PRIMARY KEY CHECK (id = 1),
    holder      TEXT    NOT NULL,
    host        TEXT    NOT NULL DEFAULT '',
    pid         INTEGER NOT NULL DEFAULT 0,
    version     TEXT    NOT NULL DEFAULT '',
    acquired_at INTEGER NOT NULL,
    renewed_at  INTEGER NOT NULL
);
