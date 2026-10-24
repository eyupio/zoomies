-- I/O wait beside the CPU figure it explains. cpu_percent counts time spent
-- waiting on disk as busy, so a host stalled on its disk reads as a host hard
-- at work; the gap between the two is the bottleneck an operator is looking
-- for. Nullable for the same reason as every other measurement here: an agent
-- too old to report it is a gap in the chart, not a quiet disk.
ALTER TABLE host_samples ADD COLUMN io_wait_percent REAL;
