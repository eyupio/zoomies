-- A personal account as an account-wide target.
--
-- GitHub keeps self-hosted runners on an organisation or on a repository, and
-- nowhere else. An App installed on a personal account therefore has no
-- account-wide place to register a runner: each one is registered on the
-- repository whose job it is for, and can only ever take that repository's
-- jobs. Two facts have to be kept for that to be scheduled correctly.
--
-- account_type is what GitHub says the installation's account is, learned by
-- the credential probe and never typed: '' until the first probe, then
-- 'organization' or 'user'. It sits beside target_type rather than widening it
-- because target_type carries a CHECK, SQLite cannot alter a CHECK in place,
-- and rebuilding installations would cascade through every table that
-- references it. It is also the more honest shape: target_type says whether
-- the installation covers an account or one repository, and this says what
-- kind of account it is.
--
-- runners.repo is the repository a runner was registered on, in owner/name
-- form, for a runner of a personal account. It is empty for every other
-- runner, which is registered on its installation's target.
ALTER TABLE installations ADD COLUMN account_type TEXT NOT NULL DEFAULT ''
    CHECK (account_type IN ('', 'organization', 'user'));
ALTER TABLE runners ADD COLUMN repo TEXT NOT NULL DEFAULT '';
