-- CPU elasticity is a policy rather than a resource limit: resources remain
-- the runner's guaranteed allocation, while this says whether unused CPU is
-- only observed or may be lent to a busy runner. JSON keeps future policy
-- fields additive without another column per tuning knob.
ALTER TABLE pools ADD COLUMN cpu_burst TEXT NOT NULL DEFAULT '{}';
