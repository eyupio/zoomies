-- Give every account and token that held administrator before the role split
-- what administrator used to mean.
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
-- day before without telling them. So what held administrator the day before
-- becomes platform here, which restores exactly the authority it had and not
-- one action more: platform is administrator plus the four that moved.
--
-- "The day before" is the load-bearing part, and it is why this is not a bare
-- `WHERE role = 'admin'`. An operator who has already run 0044 -- anyone
-- tracking main between the two, the project's own fleet among them -- can
-- since have created an account at administrator meaning the new,
-- backup-less administrator, and meaning it. Promoting that account would not
-- be restoring authority, it would be granting authority somebody deliberately
-- withheld, which is the one thing worse than the regression above. So the
-- promotion is bounded by when 0044 ran: a principal that predates the role
-- split is one whose administrator meant the old thing, and only those are
-- carried across. On the ordinary upgrade from a release the two migrations
-- run seconds apart in the same startup, so this bound excludes nothing.
--
-- The separation the role exists for is then made by an act somebody takes --
-- demoting the fleet's administrators on an instance run for another team --
-- rather than by an upgrade doing it silently. A fresh install is unaffected:
-- it has one account, the bootstrap makes it platform, and this runs once.
--
-- Disabled accounts are promoted too. A disabled account can do nothing at
-- all, and the question this answers is what it finds when somebody enables
-- it again: what it had.

UPDATE users SET role = 'platform'
WHERE role = 'admin'
  AND created_at < (SELECT applied_at FROM schema_migrations
                    WHERE name = '0044_platform_role.sql');

-- A token carries no more than the caller that made it, and every caller that
-- made one of these before the split is now platform. The same bound applies,
-- for the same reason: a token minted at administrator after 0044 was minted
-- knowing what administrator no longer reaches. Revoked tokens are left alone:
-- they authorise nothing, and rewriting them would only make the audit trail
-- read as though somebody had changed them.
UPDATE api_tokens SET role = 'platform'
WHERE role = 'admin' AND revoked = 0
  AND created_at < (SELECT applied_at FROM schema_migrations
                    WHERE name = '0044_platform_role.sql');
