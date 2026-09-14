-- The fleet's own configuration, so that changing a setting is something an
-- administrator does in the UI rather than something somebody with a shell on
-- the controller's host does to a file.
--
-- It is a separate table from `settings` on purpose, even though the two look
-- alike. That one holds facts the product keeps about itself -- the recovery
-- fence, the private-network identity -- which are not settings and must not
-- appear on a settings page, be exported with a configuration, or be cleared by
-- an operator resetting one back to its defaults. Mixing them would make every
-- one of those operations need a list of exceptions.
--
-- Only keys an operator has actually set have a row. An absent key is not the
-- same as a key set to its default: the defaults are computed on the host that
-- reads them (the database path from the state directory, the agent's capacity
-- from the number of cores, its name from the machine) and a row claiming to be
-- "the default" would freeze one host's answer for every host after it.
CREATE TABLE instance_settings (
    key        TEXT PRIMARY KEY,
    -- The value as an operator would have typed it: 30s, not 30000000000.
    value      TEXT    NOT NULL DEFAULT '',
    -- Sealed with the instance encryption key, then base64, exactly as the
    -- private-network identity in `settings` is. A credential is stored the
    -- same way wherever it is stored.
    secret     INTEGER NOT NULL DEFAULT 0,
    updated_at INTEGER NOT NULL,
    -- Who changed it last, so the settings page can say so next to the value
    -- without a join onto the audit trail for every row it draws.
    updated_by TEXT    NOT NULL DEFAULT ''
);
