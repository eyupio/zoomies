package controller

import (
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/store"
)

func TestOptimisationPolicyFollowsTheLiveSettingsAndNothingElse(t *testing.T) {
	h := newHarness(t)
	p, err := h.c.OptimisationPolicy(h.ctx)
	if err != nil || p.SourceAllowed || p.WorkflowEditsAllowed || p.PublishAllowed {
		t.Fatal("a fresh instance must allow no source reads, workflow edits or publication")
	}
	h.c.UpdateConfig(func(c *config.Config) { c.Optimisation.SourceReads = true })
	p, _ = h.c.OptimisationPolicy(h.ctx)
	if !p.SourceAllowed || p.WorkflowEditsAllowed || p.PublishAllowed {
		t.Fatal("the switches are separate grants")
	}
	h.c.UpdateConfig(func(c *config.Config) { c.Optimisation.WorkflowEdits = true })
	p, _ = h.c.OptimisationPolicy(h.ctx)
	if !p.SourceAllowed || !p.WorkflowEditsAllowed || p.PublishAllowed {
		t.Fatal("turning previews on did not reach the policy or turned publishing on")
	}
	h.c.UpdateConfig(func(c *config.Config) { c.Optimisation.Publish = true })
	p, _ = h.c.OptimisationPolicy(h.ctx)
	if !p.PublishAllowed {
		t.Fatal("turning publishing on did not reach the policy")
	}
}

func TestInspectingCheckoutEvidenceShowsSourceOnceAndRetainsNothing(t *testing.T) {
	h, inst, f, sel, review, _ := checkoutCaptureFixture(t)
	h.c.UpdateConfig(func(c *config.Config) { c.Optimisation.SourceReads, c.Optimisation.WorkflowEdits = true, true })
	got, err := h.c.InspectCheckoutEvidence(h.ctx, inst.ID, inst.Target, sel)
	if err != nil {
		t.Fatal(err)
	}
	if f.calls != 1 || f.quotaCalls != 1 || got.SourceSHA256 != review.SourceSHA256 || got.HeadSHA != review.HeadSHA || !strings.Contains(got.Content, "fetch-depth: 0") || got.CheckoutMS != 4200 {
		t.Fatalf("inspection %+v", got)
	}
	_, total, err := h.st.ListAudit(h.ctx, store.AuditFilter{Actions: []string{"optimisation.preview_retained"}}, store.Page{Limit: 10})
	if err != nil || total != 0 {
		t.Fatal("an inspection retained a preview", err)
	}
}

func TestInspectingCheckoutEvidenceIsRefusedWhileEitherSwitchIsOffBeforeAnyRead(t *testing.T) {
	for _, mode := range []string{"both_off", "source_only", "edits_only"} {
		t.Run(mode, func(t *testing.T) {
			h, inst, f, sel, _, _ := checkoutCaptureFixture(t)
			h.c.UpdateConfig(func(c *config.Config) {
				c.Optimisation.SourceReads = mode == "source_only"
				c.Optimisation.WorkflowEdits = mode == "edits_only"
			})
			if _, err := h.c.InspectCheckoutEvidence(h.ctx, inst.ID, inst.Target, sel); err != ErrOptimisationPolicy {
				t.Fatalf("want the policy refusal, got %v", err)
			}
			if f.calls != 0 || f.quotaCalls != 0 {
				t.Fatal("a refused inspection spent reads")
			}
		})
	}
}
