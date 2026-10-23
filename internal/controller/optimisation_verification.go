package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/eyupio/zoomies/internal/store"
)

var ErrOptimisationVerification = errors.New("optimisation verification is unavailable, inconsistent or unsupported")

// LinkOptimisationVerification attaches one retained workflow comparison to a
// published preview. The comparison must name the preview as its revision
// delta, and it must already be retained. The link is evidence to read, not a
// claim that the change saved time.
func (c *Controller) LinkOptimisationVerification(ctx context.Context, actor *store.AuditEvent, installation, repo, id, comparisonID string) (*RetainedOptimisationPreview, error) {
	if actor == nil || actor.ActorID == "" || installation == "" || repo == "" || !strings.HasPrefix(id, store.PrefixOptimisationPreview+"_") || !strings.HasPrefix(comparisonID, store.PrefixComparison+"_") {
		return nil, ErrOptimisationVerification
	}
	current, err := c.ReplayOptimisationPreview(ctx, installation, repo, id)
	if err != nil {
		return nil, err
	}
	if current.Publication == nil {
		return nil, fmt.Errorf("%w: publish the preview before linking a comparison", ErrOptimisationVerification)
	}
	comparison, err := c.ReplayExperimentComparison(ctx, installation, repo, comparisonID)
	if err != nil {
		return nil, err
	}
	if comparison.Report.Snapshot.Kind != "workflow" {
		return nil, fmt.Errorf("%w: link a workflow comparison, not a runner comparison", ErrOptimisationVerification)
	}
	if comparison.Report.Snapshot.RevisionDeltaID != id {
		return nil, fmt.Errorf("%w: the comparison does not name this preview as its revision change; capture it from the preview's Compare runs link", ErrOptimisationVerification)
	}
	linked, err := c.st.LinkOptimisationComparison(ctx, installation, repo, id, comparisonID, actor)
	if err != nil {
		return nil, err
	}
	current.Verification = &RetainedOptimisationVerification{ComparisonID: linked.ComparisonID, LinkedBy: linked.LinkedBy, LinkedAt: linked.CreatedAt}
	return current, nil
}
