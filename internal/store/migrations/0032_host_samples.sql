-- One row per host per minute, so the Hosts page can draw what a machine has
-- been doing rather than only what it is doing now. The fleet has had this in
-- fleet_samples since the start; a host had nothing, and a card that says
-- "CPU 93%" cannot say whether that is a spike or the whole afternoon.
--
-- Measurements a host has not reported are NULL, never zero: an agent too old
-- to measure CPU is not an idle machine, and the chart draws a gap there. The
-- controller prunes on the same retention window as fleet_samples.
CREATE TABLE host_samples (
    host_id               TEXT    NOT NULL,
    at                    INTEGER NOT NULL,
    capacity              INTEGER NOT NULL DEFAULT 0,
    active_runners        INTEGER NOT NULL DEFAULT 0,
    cpu_percent           REAL,
    load_average_1m       REAL,
    cpus                  INTEGER NOT NULL DEFAULT 0,
    memory_mb             INTEGER NOT NULL DEFAULT 0,
    memory_available_mb   INTEGER,
    allocatable_cpus      REAL    NOT NULL DEFAULT 0,
    allocatable_memory_mb INTEGER NOT NULL DEFAULT 0,
    reserved_cpus         REAL,
    reserved_memory_mb    INTEGER,
    disk_total_mb         INTEGER NOT NULL DEFAULT 0,
    disk_free_mb          INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (host_id, at)
);
-- The prune deletes by time across every host, and the primary key leads on
-- the host.
CREATE INDEX host_samples_at ON host_samples (at);
