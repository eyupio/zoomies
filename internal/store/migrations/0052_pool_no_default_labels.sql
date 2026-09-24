-- A pool may register its runners without the labels actions/runner adds on
-- its own (self-hosted, the operating system and the architecture).
--
-- NOT NULL DEFAULT 0 so every existing pool keeps the labels it has always
-- advertised, and a job that asks for self-hosted keeps landing where it did.
-- A controller rolled back past this release selects its columns by name and
-- never sees it.
ALTER TABLE pools ADD COLUMN no_default_labels INTEGER NOT NULL DEFAULT 0;
