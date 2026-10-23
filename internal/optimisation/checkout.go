// Package optimisation computes reviewable proposals from explicitly supplied
// evidence. It cannot read source, call a model, publish or dispatch a workflow.
package optimisation

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path"
	"strconv"
	"strings"

	"github.com/eyupio/zoomies/internal/kennel/workflow"
	"github.com/eyupio/zoomies/internal/prrepair"
)

const MaxCheckoutSourceBytes = 32 << 10

var ErrUnsupported = errors.New("checkout preview needs pinned regular workflow source, matching successful timing evidence, reviewed history needs and source/edit policy")

type Policy struct {
	SourceAllowed        bool
	WorkflowEditsAllowed bool
	// PublishAllowed is instance consent for opening a draft pull request for
	// an approved preview. Preview computation never reads it; only publication
	// does.
	PublishAllowed bool
}
type Source struct {
	HeadSHA string
	File    prrepair.File
}

// Timing is caller-supplied, not independently attested by this package. A future
// collector must bind the exact run, attempt and step before admitting it.
type Timing struct {
	EvidenceID   string
	HeadSHA      string
	SourceSHA256 string
	RunID        string
	Attempt      int
	JobIndex     int
	StepIndex    int
	Outcome      string
	CheckoutMS   *int64
}

// HistoryReview explicitly covers repository tooling and downstream consumers.
// Source scanning alone cannot discover every need for tags or commit history.
type HistoryReview struct {
	HeadSHA      string
	SourceSHA256 string
	Scope        string
	NeedsHistory *bool
}
type Preview struct {
	Version      string
	Kind         string
	State        string
	HeadSHA      string
	SourceSHA256 string
	AfterSHA256  string
	EvidenceID   string
	RunID        string
	Attempt      int
	JobIndex     int
	StepIndex    int
	Line         int
	CheckoutMS   int64
	Edit         prrepair.Edit
	File         prrepair.File
	Verification string
	Explanation  string
}

func hash(s string) string {
	v := sha256.Sum256([]byte(s))
	return hex.EncodeToString(v[:])
}
func commit(s string) bool {
	if len(s) != 40 || s != strings.ToLower(s) {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}
func decimal(s string) bool {
	v, err := strconv.ParseInt(s, 10, 64)
	return err == nil && v > 0 && strconv.FormatInt(v, 10) == s
}
func checkoutSource(s Source, policy Policy) bool {
	p := s.File.Path
	return policy.SourceAllowed && policy.WorkflowEditsAllowed && commit(s.HeadSHA) && len(s.File.Content) > 0 && len(s.File.Content) <= MaxCheckoutSourceBytes && strings.HasPrefix(p, ".github/workflows/") && path.Dir(p) == ".github/workflows" && (strings.HasSuffix(p, ".yml") || strings.HasSuffix(p, ".yaml"))
}

func PreviewCheckout(s Source, timing Timing, history HistoryReview, policy Policy) (Preview, error) {
	if !checkoutSource(s, policy) {
		return Preview{}, ErrUnsupported
	}
	digest := hash(s.File.Content)
	if timing.HeadSHA != s.HeadSHA || timing.SourceSHA256 != digest || timing.EvidenceID == "" || len(timing.EvidenceID) > 128 || !decimal(timing.RunID) || timing.Attempt < 1 || timing.Outcome != "success" || timing.CheckoutMS == nil || *timing.CheckoutMS <= 0 || *timing.CheckoutMS > 7*24*60*60*1000 || history.HeadSHA != s.HeadSHA || history.SourceSHA256 != digest || history.Scope != "repository_and_downstream_consumers" || history.NeedsHistory == nil || *history.NeedsHistory {
		return Preview{}, ErrUnsupported
	}
	before, after, line, err := workflow.CheckoutDepthChange([]byte(s.File.Content), timing.JobIndex, timing.StepIndex)
	if err != nil {
		return Preview{}, ErrUnsupported
	}
	edit := prrepair.Edit{Path: s.File.Path, Before: before, After: after}
	files, err := prrepair.ApplySuppliedEdits([]prrepair.File{s.File}, []prrepair.Edit{edit}, true)
	if err != nil {
		return Preview{}, ErrUnsupported
	}
	return Preview{
		Version: "checkout-preview-v1", Kind: "checkout_depth", State: "preview_ready",
		HeadSHA: s.HeadSHA, SourceSHA256: digest, AfterSHA256: hash(files[0].Content),
		EvidenceID: timing.EvidenceID, RunID: timing.RunID, Attempt: timing.Attempt,
		JobIndex: timing.JobIndex, StepIndex: timing.StepIndex, Line: line,
		CheckoutMS: *timing.CheckoutMS, Edit: edit, File: files[0],
		Verification: "inconclusive",
		Explanation:  "Reduce this checkout's explicit full-history fetch to one commit. History needs are caller-reviewed, timing is descriptive and savings are unverified. Passing CI alone cannot establish improvement. No approval, publication or experiment dispatch is implied.",
	}, nil
}

// CheckCurrent is a local precondition, not publication authorisation. The future
// publisher must independently fetch the head and recheck policy immediately.
func CheckCurrent(p Preview, s Source, policy Policy) error {
	if p.Version != "checkout-preview-v1" || p.Kind != "checkout_depth" || p.State != "preview_ready" || !checkoutSource(s, policy) || s.HeadSHA != p.HeadSHA || hash(s.File.Content) != p.SourceSHA256 || s.File.Path != p.Edit.Path {
		return ErrUnsupported
	}
	before, after, line, err := workflow.CheckoutDepthChange([]byte(s.File.Content), p.JobIndex, p.StepIndex)
	if err != nil || before != p.Edit.Before || after != p.Edit.After || line != p.Line {
		return ErrUnsupported
	}
	files, err := prrepair.ApplySuppliedEdits([]prrepair.File{s.File}, []prrepair.Edit{p.Edit}, true)
	if err != nil || files[0] != p.File || hash(files[0].Content) != p.AfterSHA256 {
		return ErrUnsupported
	}
	return nil
}
