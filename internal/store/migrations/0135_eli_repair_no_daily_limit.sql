-- Eli no longer caps how many repairs a repository or a person may start in 24
-- hours. The limit stopped repairs a person had every reason to want, and the
-- guard against a runaway loop is the deduplication on the PR's head commit and
-- the check that Eli never repairs its own repair commit, both of which read the
-- eli_repairs ledger and not this column.
ALTER TABLE eli_repair_policies DROP COLUMN daily_limit;
