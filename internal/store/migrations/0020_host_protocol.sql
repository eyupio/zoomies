-- What version of the agent protocol each host speaks, and whether this
-- controller can work with it.
--
-- The protocol was checked at join and nowhere else, so an agent that joined
-- before a protocol bump kept polling and receiving tasks it could not
-- understand. Recording it on every heartbeat is what lets the fleet notice,
-- and `incompatible` is the answer: the host is excluded from placement the
-- way a cordon excludes it, rather than refused -- a refusal at heartbeat
-- would restart every agent in the fleet at once, which is the outage the
-- upgrade was meant to avoid.
--
-- Zero means "has not said", which is what every existing row is and what an
-- agent older than this column reports. It is not incompatible: an agent that
-- never sends a version is the one case this cannot judge.
ALTER TABLE hosts ADD COLUMN protocol_version INTEGER NOT NULL DEFAULT 0;
ALTER TABLE hosts ADD COLUMN incompatible INTEGER NOT NULL DEFAULT 0;
