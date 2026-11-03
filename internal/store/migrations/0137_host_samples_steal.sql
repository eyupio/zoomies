-- Steal beside the CPU figure it explains. cpu_percent counts the time the
-- hypervisor kept the CPU from a virtual machine as busy, so a host that is
-- being starved reads as a host hard at work, and a spike that has passed leaves
-- nothing to say which it was. Nullable for the same reason as every other
-- measurement here: an agent too old to report it, or a kernel that does not, is
-- a gap in the chart and not a machine that was never starved.
ALTER TABLE host_samples ADD COLUMN steal_percent REAL;
