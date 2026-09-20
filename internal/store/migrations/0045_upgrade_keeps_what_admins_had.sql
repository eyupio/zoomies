-- Give every account and token that held administrator what administrator
-- used to mean.
--
-- 0044 promoted the oldest enabled administrator and stopped there, which was
-- enough to keep an instance from becoming one nobody can operate. It was not
-- enough to leave an existing instance working. A self-hosted fleet whose
-- operations are shared between two or three administrators would come up from
-- the upgrade with one of them able to take a backup or lift the recovery
-- fence and the others quietly unable -- and an API token minted at
-- administrator, which is what a nightly `zoomies backup` runs as, would start
-- answering 403 with nothing having been said to anybody.
--
-- An upgrade may add a role. It may not take away what somebody could do the
-- day before without telling them. So everything that held administrator
-- becomes platform here, which restores exactly the authority it had and not
-- one action more: platform is administrator plus the four that moved.
--
-- The separation the role exists for is then made by an act somebody takes --
-- demoting the fleet's administrators on an instance run for another team --
-- rather than by an upgrade doing it silently. A fresh install is unaffected:
-- it has one account, the bootstrap makes it platform, and this runs once.
--
-- Disabled accounts are promoted too. A disabled account can do nothing at
-- all, and the question this answers is what it finds when somebody enables
-- it again: what it had.

UPDATE users SET role = 'platform' WHERE role = 'admin';

-- A token carries no more than the caller that made it, and every caller that
-- made one of these is now platform. Revoked tokens are left alone: they
-- authorise nothing, and rewriting them would only make the audit trail read
-- as though somebody had changed them.
UPDATE api_tokens SET role = 'platform' WHERE role = 'admin' AND revoked = 0;
