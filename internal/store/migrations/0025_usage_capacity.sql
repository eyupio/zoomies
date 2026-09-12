-- One observation per pool/minute, recording whether placement hit host capacity.
-- Deliberately no FK: history remains meaningful after a pool is deleted.
CREATE TABLE usage_capacity_samples (
    pool_id TEXT NOT NULL,
    at INTEGER NOT NULL,
    blocked INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (pool_id, at)
);
CREATE INDEX idx_usage_capacity_at ON usage_capacity_samples(at);
