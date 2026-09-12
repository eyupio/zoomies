package controller

import (
	"context"
	"github.com/eyupio/zoomies/internal/store"
)

func (c *Controller) ControlProvisioning(ctx context.Context, ids []string, action string) ([]store.ProvisioningResult, error) {
	// Serialize with snapshot AND apply, so an old plan cannot create runners
	// after a pause has been acknowledged. Already-issued tasks are unaffected.
	c.reconcileMu.Lock()
	defer c.reconcileMu.Unlock()
	results, err := c.st.ControlProvisioning(ctx, ids, action)
	if err != nil {
		return nil, err
	}
	for _, result := range results {
		if result.OK {
			if j, err := c.st.GetJob(ctx, result.ID); err == nil {
				c.publishJob(ctx, j)
			}
		}
	}
	c.Nudge()
	return results, nil
}
