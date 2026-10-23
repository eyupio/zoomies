package optimisation

import (
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/prrepair"
)

func checkoutFixture() (Source, Timing, HistoryReview, Policy) {
	s := Source{HeadSHA: strings.Repeat("a", 40), File: prrepair.File{Path: ".github/workflows/ci.yml", Mode: "100644", Content: "name: Test\non: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@" + strings.Repeat("b", 40) + "\n        with:\n          fetch-depth: 0 # reviewed history scope\n      - run: npm test\n"}}
	duration := int64(4200)
	needs := false
	return s, Timing{EvidenceID: "checkout_fixture", HeadSHA: s.HeadSHA, SourceSHA256: hash(s.File.Content), RunID: "9223372036854775807", Attempt: 2, JobIndex: 0, StepIndex: 0, Outcome: "success", CheckoutMS: &duration}, HistoryReview{HeadSHA: s.HeadSHA, SourceSHA256: hash(s.File.Content), Scope: "repository_and_downstream_consumers", NeedsHistory: &needs}, Policy{SourceAllowed: true, WorkflowEditsAllowed: true}
}
func TestCheckoutPreviewPreservesUnrelatedBytesAndNeverClaimsVerifiedSavings(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n"} {
		s, timing, history, policy := checkoutFixture()
		s.File.Content = strings.ReplaceAll(s.File.Content, "\n", ending)
		timing.SourceSHA256 = hash(s.File.Content)
		history.SourceSHA256 = timing.SourceSHA256
		p, err := PreviewCheckout(s, timing, history, policy)
		if err != nil {
			t.Fatal(err)
		}
		want := strings.Replace(s.File.Content, "fetch-depth: 0", "fetch-depth: 1", 1)
		if p.File.Content != want || p.File.Mode != "100644" || p.Line != 9 || p.Verification != "inconclusive" || p.RunID != timing.RunID || p.CheckoutMS != 4200 || p.AfterSHA256 != hash(want) || CheckCurrent(p, s, policy) != nil {
			t.Fatalf("incorrect preview: %+v", p)
		}
		p.File.Content += "# unrelated change\n"
		if CheckCurrent(p, s, policy) == nil {
			t.Fatal("tampered preview accepted")
		}
	}
}
func TestCheckoutPreviewRefusesUnknownOrStaleEvidenceAndPolicy(t *testing.T) {
	for _, mode := range []string{"source_policy", "edit_policy", "head", "source_digest", "unknown_timing", "failed_timing", "zero_timing", "unknown_history", "required_history", "history_head", "history_digest", "history_scope", "run_id", "attempt", "step", "path", "partial", "mode", "oversize", "already_shallow", "floating_action", "ref_override", "tags", "duplicate_preimage", "credentials"} {
		t.Run(mode, func(t *testing.T) {
			s, timing, history, policy := checkoutFixture()
			switch mode {
			case "source_policy":
				policy.SourceAllowed = false
			case "edit_policy":
				policy.WorkflowEditsAllowed = false
			case "head":
				timing.HeadSHA = strings.Repeat("c", 40)
			case "source_digest":
				timing.SourceSHA256 = strings.Repeat("c", 64)
			case "unknown_timing":
				timing.CheckoutMS = nil
			case "failed_timing":
				timing.Outcome = "failure"
			case "zero_timing":
				*timing.CheckoutMS = 0
			case "unknown_history":
				history.NeedsHistory = nil
			case "required_history":
				*history.NeedsHistory = true
			case "history_head":
				history.HeadSHA = strings.Repeat("c", 40)
			case "history_digest":
				history.SourceSHA256 = strings.Repeat("c", 64)
			case "history_scope":
				history.Scope = "root_file_only"
			case "run_id":
				timing.RunID = "01"
			case "attempt":
				timing.Attempt = 0
			case "step":
				timing.StepIndex = 1
			case "path":
				s.File.Path = ".github/workflows/../ci.yml"
			case "partial":
				s.File.Partial = true
			case "mode":
				s.File.Mode = "120000"
			case "oversize":
				s.File.Content += strings.Repeat("#", MaxCheckoutSourceBytes)
			case "already_shallow":
				s.File.Content = strings.Replace(s.File.Content, "fetch-depth: 0", "fetch-depth: 1", 1)
			case "floating_action":
				s.File.Content = strings.Replace(s.File.Content, strings.Repeat("b", 40), "main", 1)
			case "ref_override":
				s.File.Content = strings.Replace(s.File.Content, "          fetch-depth:", "          ref: main\n          fetch-depth:", 1)
			case "tags":
				s.File.Content = strings.Replace(s.File.Content, "          fetch-depth:", "          fetch-tags: true\n          fetch-depth:", 1)
			case "duplicate_preimage":
				job := s.File.Content[strings.Index(s.File.Content, "  build:"):]
				s.File.Content += strings.Replace(job, "  build:", "  other:", 1)
			case "credentials":
				s.File.Content += "# ghp_" + strings.Repeat("x", 30) + "\n"
			}
			if mode == "oversize" || mode == "already_shallow" || mode == "floating_action" || mode == "ref_override" || mode == "tags" || mode == "duplicate_preimage" || mode == "credentials" {
				timing.SourceSHA256 = hash(s.File.Content)
				history.SourceSHA256 = timing.SourceSHA256
			}
			if p, err := PreviewCheckout(s, timing, history, policy); err == nil || p.File.Content != "" {
				t.Fatal("unsupported preview emitted source")
			}
		})
	}
}
func TestCheckoutCurrentnessStopsWhenHeadSourceOrPolicyChanges(t *testing.T) {
	for _, mode := range []string{"head", "source", "policy", "path", "edit", "version"} {
		s, timing, history, policy := checkoutFixture()
		p, err := PreviewCheckout(s, timing, history, policy)
		if err != nil {
			t.Fatal(err)
		}
		switch mode {
		case "head":
			s.HeadSHA = strings.Repeat("c", 40)
		case "source":
			s.File.Content += "# advanced source\n"
		case "policy":
			policy.WorkflowEditsAllowed = false
		case "path":
			s.File.Path = ".github/workflows/other.yml"
		case "edit":
			p.Edit.After = "          fetch-depth: 2\n"
		case "version":
			p.Version = "future"
		}
		if CheckCurrent(p, s, policy) == nil {
			t.Fatalf("%s remained current", mode)
		}
	}
}
