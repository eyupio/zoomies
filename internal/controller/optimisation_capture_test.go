package controller

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/optimisation"
	"github.com/eyupio/zoomies/internal/prrepair"
	"github.com/eyupio/zoomies/internal/store"
)

type checkoutCaptureReader struct {
	github.Client
	evidence          *github.CheckoutEvidence
	quota             *github.RateLimit
	calls, quotaCalls int
	deadline          bool
	err               error
	publishes         int
	publishRequest    github.OptimisationPublishRequest
	publishPR         *github.PullRequest
	publishErr        error
}

func (f *checkoutCaptureReader) RateLimit(context.Context) (*github.RateLimit, error) {
	f.quotaCalls++
	return f.quota, nil
}
func (f *checkoutCaptureReader) ReadCheckoutEvidence(ctx context.Context, _ string, _ github.CheckoutSelection) (*github.CheckoutEvidence, error) {
	f.calls++
	_, f.deadline = ctx.Deadline()
	return f.evidence, f.err
}
func (f *checkoutCaptureReader) PublishOptimisation(_ context.Context, req github.OptimisationPublishRequest) (*github.PullRequest, error) {
	f.publishes++
	f.publishRequest = req
	return f.publishPR, f.publishErr
}
func checkoutCaptureFixture(t *testing.T) (*harness, *store.Installation, *checkoutCaptureReader, github.CheckoutSelection, optimisation.HistoryReview, *store.AuditEvent) {
	t.Helper()
	h, inst, _ := experimentJoinFixture(t)
	sel := github.CheckoutSelection{RunID: 10, Attempt: 2, GitHubJobID: 100, JobIndex: 0, StepIndex: 0}
	source := optimisation.Source{HeadSHA: strings.Repeat("a", 40), File: prrepair.File{Path: ".github/workflows/private.yml", Mode: "100644", Content: "on: push\njobs:\n  build:\n    name: Build\n    runs-on: ubuntu-latest\n    steps:\n      - name: Checkout\n        uses: actions/checkout@" + strings.Repeat("b", 40) + "\n        with:\n          fetch-depth: 0\n"}}
	hash := func(s string) string { v := sha256.Sum256([]byte(s)); return hex.EncodeToString(v[:]) }
	blob := sha1.Sum([]byte("blob " + strconv.Itoa(len(source.File.Content)) + "\x00" + source.File.Content))
	ms, needs := int64(4200), false
	e := &github.CheckoutEvidence{Version: "checkout-evidence-v1", Basis: "push_source_unique_literal_names", Repository: inst.Target, Selection: sel, StepNumber: 42, BlobSHA: hex.EncodeToString(blob[:]), JobNameSHA256: hash("Build"), StepNameSHA256: hash("Checkout"), HeadBranch: "main", Source: source, Timing: optimisation.Timing{EvidenceID: "checkout:10:2:100:42", HeadSHA: source.HeadSHA, SourceSHA256: hash(source.File.Content), RunID: "10", Attempt: 2, JobIndex: 0, StepIndex: 0, Outcome: "success", CheckoutMS: &ms}}
	f := &checkoutCaptureReader{Client: h.gh.Client(inst.Target, inst.TargetType), evidence: e, quota: &github.RateLimit{Limit: 100, Remaining: 100}}
	h.c.clients.entries[inst.ID] = &clientEntry{client: f, updatedAt: inst.UpdatedAt.Truncate(1e6)}
	return h, inst, f, sel, optimisation.HistoryReview{HeadSHA: source.HeadSHA, SourceSHA256: hash(source.File.Content), Scope: "repository_and_downstream_consumers", NeedsHistory: &needs}, &store.AuditEvent{ActorID: "usr_operator", ActorKind: "user"}
}
func TestCheckoutCaptureChecksServerPolicyTwiceAndReplaysWithoutMoreReads(t *testing.T) {
	h, inst, f, sel, review, actor := checkoutCaptureFixture(t)
	checks := 0
	check := func(context.Context) (optimisation.Policy, error) {
		checks++
		return optimisation.Policy{SourceAllowed: true, WorkflowEditsAllowed: true}, nil
	}
	saved, err := h.c.CaptureOptimisationPreview(h.ctx, actor, inst.ID, inst.Target, sel, review, check)
	if err != nil {
		t.Fatal(err)
	}
	if checks != 2 || f.calls != 1 || f.quotaCalls != 1 || !f.deadline || saved.Preview.Verification != "inconclusive" || saved.Currentness != "not_checked" || saved.Collection == nil || saved.Collection.SourceFunding != "installation_github_read" || saved.Collection.StepNumber != 42 {
		t.Fatal("admission or authority changed")
	}
	replay, err := h.c.ReplayOptimisationPreview(h.ctx, inst.ID, inst.Target, saved.ID)
	if err != nil || f.calls != 1 || replay == nil || replay.Collection == nil || replay.Collection.StepNumber != 42 {
		t.Fatal("replay read replacement evidence", err)
	}
}
func TestCheckoutCaptureRefusesPolicyWithdrawalScopeUnknownReviewQuotaAndInvalidReaderFacts(t *testing.T) {
	for _, mode := range []string{"policy_before", "policy_after", "scope", "unknown_history", "required_history", "quota", "foreign_reader", "bad_timing", "history_digest", "reader_error", "shared_flight"} {
		t.Run(mode, func(t *testing.T) {
			h, inst, f, sel, review, actor := checkoutCaptureFixture(t)
			repo := inst.Target
			checks := 0
			check := func(context.Context) (optimisation.Policy, error) {
				checks++
				return optimisation.Policy{SourceAllowed: mode != "policy_before", WorkflowEditsAllowed: mode != "policy_after" || checks == 1}, nil
			}
			switch mode {
			case "scope":
				repo = "other/repo"
			case "unknown_history":
				review.NeedsHistory = nil
			case "required_history":
				*review.NeedsHistory = true
			case "quota":
				f.quota.Remaining = 56
			case "foreign_reader":
				f.evidence.Repository = "other/repo"
			case "bad_timing":
				f.evidence.Timing.SourceSHA256 = strings.Repeat("d", 64)
			case "history_digest":
				review.SourceSHA256 = strings.Repeat("d", 64)
			case "reader_error":
				f.err = context.Canceled
			case "shared_flight":
				release, err := h.c.admitPRDelay(inst.ID)
				if err != nil {
					t.Fatal(err)
				}
				defer release()
			}
			if p, err := h.c.CaptureOptimisationPreview(h.ctx, actor, inst.ID, repo, sel, review, check); err == nil || p != nil {
				t.Fatal("refused capture retained preview")
			}
			_, total, err := h.st.ListAudit(h.ctx, store.AuditFilter{Actions: []string{"optimisation.preview_retained"}}, store.Page{Limit: 10})
			if err != nil || total != 0 {
				t.Fatal("refusal committed history", err)
			}
			if mode == "policy_before" || mode == "scope" || mode == "unknown_history" || mode == "required_history" || mode == "quota" || mode == "shared_flight" {
				if f.calls != 0 {
					t.Fatal("preflight refusal spent reads")
				}
			}
			if mode == "reader_error" {
				f.err = nil
				if _, err := h.c.CaptureOptimisationPreview(h.ctx, actor, inst.ID, repo, sel, review, check); err != nil {
					t.Fatal("failed read leaked shared flight", err)
				}
			}
		})
	}
}
func TestCheckoutCapturePreservesTheHalfQuotaBoundary(t *testing.T) {
	h, inst, f, sel, review, actor := checkoutCaptureFixture(t)
	f.quota.Remaining = 57
	_, err := h.c.CaptureOptimisationPreview(h.ctx, actor, inst.ID, inst.Target, sel, review, func(context.Context) (optimisation.Policy, error) {
		return optimisation.Policy{SourceAllowed: true, WorkflowEditsAllowed: true}, nil
	})
	if err != nil || f.calls != 1 {
		t.Fatal("exact half-quota boundary refused", err)
	}
}

func TestCapturedCheckoutProvenanceCannotChangeDuringReplayEvenWithAValidStorageDigest(t *testing.T) {
	h, inst, f, sel, review, actor := checkoutCaptureFixture(t)
	saved, err := h.c.CaptureOptimisationPreview(h.ctx, actor, inst.ID, inst.Target, sel, review, func(context.Context) (optimisation.Policy, error) {
		return optimisation.Policy{SourceAllowed: true, WorkflowEditsAllowed: true}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	row, err := h.st.GetOptimisationPreview(h.ctx, inst.ID, inst.Target, saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"job", "step", "name_hash", "blob", "funding"} {
		var e optimisationRetainedEnvelope
		if err := json.Unmarshal(row.Payload, &e); err != nil {
			t.Fatal(err)
		}
		copy := *row
		copy.ID = store.NewID(store.PrefixOptimisationPreview)
		e.ID = copy.ID
		switch mode {
		case "job":
			e.Collection.Selection.GitHubJobID++
		case "step":
			e.Collection.StepNumber++
		case "name_hash":
			e.Collection.JobNameSHA256 = strings.Repeat("d", 64)
		case "blob":
			e.Collection.BlobSHA = strings.Repeat("d", 40)
		case "funding":
			e.Collection.SourceFunding = "someone_else"
		}
		copy.Payload, _ = json.Marshal(e)
		if err := h.st.PutOptimisationPreview(h.ctx, &copy, actor); err != nil {
			t.Fatal(err)
		}
		if _, err := h.c.ReplayOptimisationPreview(h.ctx, inst.ID, inst.Target, copy.ID); err == nil {
			t.Fatalf("%s provenance accepted", mode)
		}
	}
	if f.calls != 1 {
		t.Fatal("replay called source reader")
	}
}
