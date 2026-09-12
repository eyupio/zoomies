package controller

import (
	"context"

	"github.com/eyupio/zoomies/internal/agent"
)

// Called under reconcileMu. A removal is not complete until the host says it
// is: recording a terminal job, restarting, or exhausting a lease is not proof.
func (c *Controller) recoverHostCleanup(ctx context.Context) error {
	const batch = 100
	runners, err := c.st.PendingHostCleanup(ctx, c.cleanupCursor, batch)
	if err != nil {
		return err
	}
	for _, r := range runners {
		c.enqueueLifecycle(ctx, r.HostID, agent.Task{
			Kind: agent.TaskRemoveRunner, RunnerID: r.ID, Backend: c.backendKind(ctx, r, nil),
		})
		c.cleanupCursor = r.ID
	}
	if len(runners) < batch {
		c.cleanupCursor = ""
	}
	return nil
}
