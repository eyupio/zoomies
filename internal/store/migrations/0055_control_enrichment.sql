-- Optional GitHub lookups are durable work, never part of an ingestion ACK.
CREATE TABLE control_enrichment (
 kind TEXT NOT NULL,
 target TEXT NOT NULL,
 generation INTEGER NOT NULL DEFAULT 1,
 next_attempt INTEGER NOT NULL DEFAULT 0,
 PRIMARY KEY (kind, target)
);
CREATE INDEX control_enrichment_due ON control_enrichment(next_attempt);
