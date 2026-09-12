ALTER TABLE jobs ADD COLUMN provisioning TEXT NOT NULL DEFAULT '' CHECK (provisioning IN ('', 'paused', 'deleted'));
ALTER TABLE jobs ADD COLUMN provision_now INTEGER NOT NULL DEFAULT 0 CHECK (provision_now IN (0,1));
CREATE INDEX jobs_provisioning_queue ON jobs(state, provisioning, provision_now, queued_at, id);
CREATE INDEX runners_provisioning_order ON runners(pool_id, created_at);
