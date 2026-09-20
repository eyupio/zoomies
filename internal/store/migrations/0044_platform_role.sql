-- A fourth role, above administrator, for whoever runs the process rather
-- than the fleet.
--
-- An administrator today is the top rung: they read the bind address, the TLS
-- file paths and the database path on GET /settings, change every timer and
-- every security setting through PATCH /settings, take a support bundle with
-- the process's own paths and heap in it, lift the recovery fence and restore
-- a backup. On an instance where one team runs the controller and another
-- uses the fleet -- a platform team operating Zoomies for a product team --
-- that is more than the fleet's administrator should hold, and there is no
-- role to give the other half instead. This adds one.
--
-- Both role columns carry a CHECK naming the three roles, and SQLite cannot
-- alter a CHECK in place, so each table is rebuilt (decision 7). The copy is
-- column-for-column: neither table has changed shape since 0001, and every
-- index is recreated below.
--
-- The oldest enabled administrator becomes the platform identity. On every
-- instance that exists today one team runs both halves, and the account that
-- installed it is the one that has been changing the timers all along; the
-- alternative -- an upgrade after which nobody can change a timer, lift a
-- fence or restore a backup -- is a lock-out that needs a shell to undo.

CREATE TABLE users_new (
    id                   TEXT PRIMARY KEY,
    username             TEXT    NOT NULL,
    email                TEXT    NOT NULL DEFAULT '',
    display_name         TEXT    NOT NULL DEFAULT '',
    role                 TEXT    NOT NULL CHECK (role IN ('viewer','operator','admin','platform')),
    password_hash        TEXT    NOT NULL DEFAULT '',
    oidc_subject         TEXT    NOT NULL DEFAULT '',
    disabled             INTEGER NOT NULL DEFAULT 0,
    must_change_password INTEGER NOT NULL DEFAULT 0,
    created_at           INTEGER NOT NULL,
    last_login_at        INTEGER
);
INSERT INTO users_new
    SELECT id, username, email, display_name, role, password_hash, oidc_subject,
           disabled, must_change_password, created_at, last_login_at
    FROM users;

-- The sessions rows are copied aside and put back, because dropping a table
-- a foreign key points at deletes them on the way through: sessions.user_id
-- is ON DELETE CASCADE, and the implicit delete a DROP performs fires it.
-- Signing every operator out in the middle of an upgrade would be a
-- surprising way to learn that.
CREATE TEMPORARY TABLE sessions_carried AS SELECT * FROM sessions;

DROP TABLE users;
ALTER TABLE users_new RENAME TO users;
CREATE UNIQUE INDEX idx_users_username ON users(username);
CREATE INDEX idx_users_oidc ON users(oidc_subject);

DELETE FROM sessions;
INSERT INTO sessions SELECT * FROM sessions_carried;
DROP TABLE sessions_carried;

CREATE TABLE api_tokens_new (
    id           TEXT PRIMARY KEY,
    name         TEXT    NOT NULL,
    role         TEXT    NOT NULL CHECK (role IN ('viewer','operator','admin','platform')),
    user_id      TEXT    NOT NULL DEFAULT '',
    scopes       TEXT    NOT NULL DEFAULT '[]',
    token_hash   TEXT    NOT NULL,
    prefix       TEXT    NOT NULL DEFAULT '',
    revoked      INTEGER NOT NULL DEFAULT 0,
    created_at   INTEGER NOT NULL,
    expires_at   INTEGER,
    last_used_at INTEGER
);
INSERT INTO api_tokens_new
    SELECT id, name, role, user_id, scopes, token_hash, prefix, revoked,
           created_at, expires_at, last_used_at
    FROM api_tokens;
DROP TABLE api_tokens;
ALTER TABLE api_tokens_new RENAME TO api_tokens;
CREATE UNIQUE INDEX idx_api_tokens_hash ON api_tokens(token_hash);

UPDATE users SET role = 'platform'
 WHERE id = (SELECT id FROM users
              WHERE role = 'admin' AND disabled = 0
              ORDER BY created_at, id
              LIMIT 1);
