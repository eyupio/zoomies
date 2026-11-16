package controller

import (
	"context"
	"fmt"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/naming"
	"github.com/eyupio/zoomies/internal/scheduler"
)

// hostDiskProblems asks the placement ledger, not the doctor's fixed
// filesystem threshold. A pool can still have capacity elsewhere when one
// host has lost its slots, so this is gathered independently of pool problems.
func (c *Controller) hostDiskProblems(ctx context.Context, out *[]Problem) error {
	hosts, err := c.fleetHosts(ctx)
	if err != nil {
		return fmt.Errorf("listing hosts: %w", err)
	}
	pools, err := c.fleetPools(ctx)
	if err != nil {
		return fmt.Errorf("listing pools: %w", err)
	}
	runners, err := c.st.SchedulingRunners(ctx)
	if err != nil {
		return fmt.Errorf("listing scheduling runners: %w", err)
	}
	for _, b := range scheduler.DiskBlocks(hosts, c.sizingPools(pools), runners, c.Now()) {
		*out = append(*out, Problem{
			Code:     "host.low_disk",
			Severity: config.SeverityError,
			Title:    fmt.Sprintf("host %s is short of disk for new runners", naming.ForSentence(b.HostName)),
			Detail:   b.Detail(),
			Fix: "inspect disk use on this host, then reclaim unused images and build caches or enlarge the work filesystem. " +
				"Review Zoomies pool caches too. Lower a pool's resources.disk_gb only if its jobs need less working room. " +
				"Admission resumes automatically when enough room is available; no uncordon or restart is needed. " +
				"The doctor's disk.space check is a filesystem baseline and can pass while these placement requirements do not fit.",
			TargetKind: "host", TargetID: b.HostID,
		})
	}
	return nil
}
