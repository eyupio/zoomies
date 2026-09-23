-- A host's runtime cooldown and its last image that would not pull, kept on
-- the row so they survive a controller restart and reach the host card.
--
-- Nullable with no default, so adding it rewrites nothing on an existing
-- database, and NULL reads as "nothing has happened here" -- which is exactly
-- what every existing host can truthfully say. A controller rolled back past
-- this release selects its columns by name and never sees it.
ALTER TABLE hosts ADD COLUMN incidents TEXT;
