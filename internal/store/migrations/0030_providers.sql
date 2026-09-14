-- Providers rent machines; machines become hosts.
--
-- The machine row exists before anything at the provider does. That ordering is
-- the whole design: a create whose outcome is unknown is answered by looking for
-- the identity this row already carries, never by creating a second machine. A
-- timeout is not evidence that creation failed.
--
-- The in-flight operation lives on the machine row rather than in an operations
-- table, for the reason 0015_cleanup_record.sql gives about runners.cleanup_error:
-- the machine is what the failure is about, and the row is what an operator
-- already has open. What the row may never lose is the identity.
--
-- Ownership is deliberately not a label on the host. Labels are the operator's,
-- writable through PATCH /hosts/{id}, and a host that can be labelled into
-- deletion authority is a host anyone with the operator role can have destroyed.
-- Deletion authority is the existence of a machine row this controller wrote,
-- before the resource existed.

CREATE TABLE providers (
    id                    TEXT PRIMARY KEY,
    kind                  TEXT    NOT NULL CHECK (kind IN ('proxmox','fake')),
    name                  TEXT    NOT NULL,
    endpoint              TEXT    NOT NULL DEFAULT '',
    ca_pem                TEXT    NOT NULL DEFAULT '',
    insecure_skip_verify  INTEGER NOT NULL DEFAULT 0 CHECK (insecure_skip_verify IN (0,1)),
    settings              TEXT    NOT NULL DEFAULT '{}',   -- non-secret, keyed by SettingSpec.Key
    credentials_enc       BLOB,                            -- AES-256-GCM under the instance key
    -- The one machine shape this provider offers. A second shape is a second
    -- provider row. Letting one row mean two different machines would make the
    -- accounting ambiguous in the one place it has to be exact, so a table of
    -- machine classes is deferred to ZF-215/216 rather than half-built here.
    machine_labels        TEXT    NOT NULL DEFAULT '{}',
    machine_capacity      INTEGER NOT NULL DEFAULT 2,
    machine_backend       TEXT    NOT NULL DEFAULT 'docker',
    machine_platform      TEXT    NOT NULL DEFAULT '{}',
    machine_cpus          REAL    NOT NULL DEFAULT 0,
    machine_memory_mb     INTEGER NOT NULL DEFAULT 0,
    machine_disk_mb       INTEGER NOT NULL DEFAULT 0,
    pool_selector         TEXT    NOT NULL DEFAULT '{}',   -- which pools this provider may buy for
    max_machines          INTEGER NOT NULL DEFAULT 0,      -- 0 refuses everything: the safe default
    max_creates_in_flight INTEGER NOT NULL DEFAULT 1,
    idle_timeout_ms       INTEGER NOT NULL DEFAULT 900000,
    cost_per_machine_hour REAL    NOT NULL DEFAULT 0,
    enabled               INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
    paused                INTEGER NOT NULL DEFAULT 0 CHECK (paused IN (0,1)),
    paused_reason         TEXT    NOT NULL DEFAULT '',
    paused_until          INTEGER,                         -- set by the breaker, cleared by a success
    consecutive_failures  INTEGER NOT NULL DEFAULT 0,
    last_check_at         INTEGER,
    last_check_error      TEXT    NOT NULL DEFAULT '',
    last_sweep_at         INTEGER,
    created_at            INTEGER NOT NULL,
    updated_at            INTEGER NOT NULL
);
CREATE UNIQUE INDEX idx_providers_name ON providers(name);

CREATE TABLE machines (
    id            TEXT PRIMARY KEY,
    -- RESTRICT, not CASCADE: deleting a provider row must not silently orphan
    -- running VMs. The API refuses with a 409 naming how many are still live.
    provider_id   TEXT NOT NULL REFERENCES providers(id) ON DELETE RESTRICT,
    name          TEXT NOT NULL,
    state         TEXT NOT NULL CHECK (state IN ('planned','creating','starting',
                      'bootstrapping','enrolling','ready','draining','deleting',
                      'deleted','failed','quarantined')),
    message       TEXT NOT NULL DEFAULT '',
    -- pool_id records which pool's unmet demand asked for this machine. It is
    -- not ownership: a machine serves every pool its labels match. It exists so
    -- the machine page can answer "why does this exist".
    pool_id       TEXT REFERENCES pools(id) ON DELETE SET NULL,

    -- Identity and ownership, written before the first call that can create
    -- anything, and never rewritten.
    owner_controller_id   TEXT NOT NULL DEFAULT '',
    owner_fingerprint     TEXT NOT NULL DEFAULT '',
    resource_zone         TEXT NOT NULL DEFAULT '',  -- node/region: "pve-1"
    resource_id           TEXT NOT NULL DEFAULT '',  -- provider-native: "143"
    resource_detail       TEXT NOT NULL DEFAULT '{}',
    address               TEXT NOT NULL DEFAULT '',
    ownership_verified_at INTEGER,
    ownership_error       TEXT NOT NULL DEFAULT '',

    -- The enrolment link. host_id is the only thing that grants deletion
    -- authority over a host, and it is written by the redemption of a token this
    -- row minted -- never by a label an operator could set.
    host_id       TEXT REFERENCES hosts(id) ON DELETE SET NULL,
    join_token_id TEXT NOT NULL DEFAULT '',
    capacity      INTEGER NOT NULL DEFAULT 0,
    labels        TEXT NOT NULL DEFAULT '{}',

    -- The one operation that may be in flight. op_id is ours and is what a log
    -- line quotes; op_handle is the provider's, and is what recovery asks about.
    -- op_holder is the controller that claimed it: a database claim is what
    -- stops two controllers cloning one VM.
    op_id              TEXT NOT NULL DEFAULT '',
    op_kind            TEXT NOT NULL DEFAULT '' CHECK (op_kind IN ('','create','start','stop','bootstrap','delete')),
    op_handle          TEXT NOT NULL DEFAULT '',
    op_holder          TEXT NOT NULL DEFAULT '',
    op_started_at      INTEGER,
    op_deadline_at     INTEGER,
    op_outcome_unknown INTEGER NOT NULL DEFAULT 0 CHECK (op_outcome_unknown IN (0,1)),

    -- Retry accounting. Two error columns, because two independent systems can
    -- fail and a success on one must not erase the other's complaint (0022).
    attempts        INTEGER NOT NULL DEFAULT 0,
    next_attempt_at INTEGER,
    provider_error  TEXT NOT NULL DEFAULT '',
    bootstrap_error TEXT NOT NULL DEFAULT '',

    -- One stamp per phase entered; the machine page's timeline is derived from
    -- these and therefore cannot disagree with the state.
    created_at             INTEGER NOT NULL,
    updated_at             INTEGER NOT NULL,
    reservation_expires_at INTEGER,
    create_started_at      INTEGER,
    created_ok_at          INTEGER,
    started_at             INTEGER,
    bootstrapped_at        INTEGER,
    enrolled_at            INTEGER,
    ready_at               INTEGER,
    idle_since             INTEGER,
    draining_at            INTEGER,
    delete_started_at      INTEGER,
    -- deleted_at is when the provider CONFIRMED the resource was gone, by an
    -- inspect that could not find it. A 200 from the delete call is not that.
    deleted_at             INTEGER
);
CREATE UNIQUE INDEX idx_machines_name     ON machines(provider_id, name);
CREATE UNIQUE INDEX idx_machines_resource ON machines(provider_id, resource_zone, resource_id)
    WHERE resource_id <> '';
-- At most one machine per host, and no column on hosts an operator could set.
CREATE UNIQUE INDEX idx_machines_host     ON machines(host_id) WHERE host_id IS NOT NULL;
CREATE INDEX idx_machines_state ON machines(state);
-- The two recovery queries as partial indexes, so finding unfinished work never
-- scans history (0024's shape).
CREATE INDEX idx_machines_pending_op ON machines(id) WHERE op_id <> '';
CREATE INDEX idx_machines_owned      ON machines(id) WHERE resource_id <> '' AND deleted_at IS NULL;

-- A join token minted for one machine may only be redeemed by that machine. A
-- credential that lives inside a guest is readable by more people than one an
-- operator pastes, and scoping it is what stops a copied VM image joining as
-- somebody else.
ALTER TABLE join_tokens ADD COLUMN machine_id    TEXT NOT NULL DEFAULT '';
ALTER TABLE join_tokens ADD COLUMN expected_name TEXT NOT NULL DEFAULT '';
