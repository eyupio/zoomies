package optimisation

import (
	"errors"
	"strconv"
	"strings"

	"github.com/eyupio/zoomies/internal/kennel/workflow"
	"github.com/eyupio/zoomies/internal/prrepair"
)

// ErrReadinessUnsupported is the one refusal for a bounded readiness preview.
// It names the shape the change needs without guessing a condition or a shell.
var ErrReadinessUnsupported = errors.New("bounded readiness preview needs one fixed sleep step on an Ubuntu runner, an operator-supplied readiness command, repeated wait evidence and source/edit policy")

// ReadinessSource is the workflow file at a pinned head. The readiness command
// is supplied separately because it is a decision about the repository's
// service, not something this package may infer from a sleep.
type ReadinessSource struct {
	HeadSHA  string
	Workflow prrepair.File
}

// ReadinessEvidence is caller-supplied, not independently attested. The
// condition is the operator's command; the wait samples describe what the
// fixed sleep cost in the selected job's history.
type ReadinessEvidence struct {
	EvidenceID   string
	HeadSHA      string
	SourceSHA256 string
	Runs         int
	WaitMS       *int64
	Condition    string
}

// ReadinessPreview is a proposed replacement of one fixed sleep with a bounded
// poll of an operator-supplied condition. Verification is always inconclusive.
type ReadinessPreview struct {
	Version      string
	Kind         string
	State        string
	HeadSHA      string
	SourceSHA256 string
	AfterSHA256  string
	EvidenceID   string
	JobIndex     int
	StepIndex    int
	JobName      string
	StepName     string
	Line         int
	WaitSeconds  int
	Runs         int
	WaitMS       int64
	Condition    string
	Edit         prrepair.Edit
	File         prrepair.File
	Verification string
	Explanation  string
}

// PreviewReadiness computes the exact replacement. It never guesses a health
// command: the operator supplies the condition, and the original sleep becomes
// the bound. A condition that is not one line, or a source outside the root
// workflow, is refused rather than repaired.
func PreviewReadiness(source ReadinessSource, evidence ReadinessEvidence, policy Policy) (ReadinessPreview, error) {
	if !policy.SourceAllowed || !policy.WorkflowEditsAllowed || !commit(source.HeadSHA) || !cacheWorkflow(source.Workflow) {
		return ReadinessPreview{}, ErrReadinessUnsupported
	}
	sourceDigest := hash(source.Workflow.Content)
	condition := strings.TrimSpace(evidence.Condition)
	if condition == "" || len(condition) > 1024 || strings.ContainsAny(condition, "\r\n\x00") || evidence.HeadSHA != source.HeadSHA || evidence.SourceSHA256 != sourceDigest || evidence.EvidenceID == "" || len(evidence.EvidenceID) > 128 || strings.ContainsAny(evidence.EvidenceID, " \t\r\n\x00") || evidence.Runs < 2 || evidence.Runs > 100 || evidence.WaitMS == nil || *evidence.WaitMS <= 0 || *evidence.WaitMS > 7*24*60*60*1000 {
		return ReadinessPreview{}, ErrReadinessUnsupported
	}
	job, step, seconds, err := workflow.FindFixedSleep([]byte(source.Workflow.Content))
	if err != nil {
		return ReadinessPreview{}, ErrReadinessUnsupported
	}
	jobName, stepName, err := workflow.ReadinessNames([]byte(source.Workflow.Content), job, step)
	if err != nil {
		return ReadinessPreview{}, ErrReadinessUnsupported
	}
	loop := "until " + condition + "; do sleep 1; done"
	script := "timeout " + strconv.Itoa(seconds) + " sh -c " + shellQuote(loop)
	before, after, line, err := workflow.ReadinessChange([]byte(source.Workflow.Content), job, step, script)
	if err != nil {
		return ReadinessPreview{}, ErrReadinessUnsupported
	}
	edit := prrepair.Edit{Path: source.Workflow.Path, Before: before, After: after}
	files, err := prrepair.ApplySuppliedEdits([]prrepair.File{source.Workflow}, []prrepair.Edit{edit}, true)
	if err != nil {
		return ReadinessPreview{}, ErrReadinessUnsupported
	}
	return ReadinessPreview{
		Version: "readiness-preview-v1", Kind: "bounded_readiness_poll", State: "preview_ready",
		HeadSHA: source.HeadSHA, SourceSHA256: sourceDigest, AfterSHA256: hash(files[0].Content),
		EvidenceID: evidence.EvidenceID, JobIndex: job, StepIndex: step, JobName: jobName, StepName: stepName, Line: line,
		WaitSeconds: seconds, Runs: evidence.Runs, WaitMS: *evidence.WaitMS, Condition: condition, Edit: edit, File: files[0],
		Verification: "inconclusive",
		Explanation:  "Replace a fixed " + strconv.Itoa(seconds) + "s sleep in " + stepName + " with a poll of the command the operator supplied, bounded to the same " + strconv.Itoa(seconds) + "s. The wait step averaged " + setupSeconds(*evidence.WaitMS) + " over " + runCount(evidence.Runs) + ". The condition is not inferred, and the saving is unverified: the service may become ready later or fail the bound.",
	}, nil
}

// CheckReadinessCurrent is a local precondition, not publication authorisation.
func CheckReadinessCurrent(p ReadinessPreview, source ReadinessSource, evidence ReadinessEvidence, policy Policy) error {
	if p.Version != "readiness-preview-v1" || p.Kind != "bounded_readiness_poll" || p.State != "preview_ready" {
		return ErrReadinessUnsupported
	}
	current, err := PreviewReadiness(source, evidence, policy)
	if err != nil {
		return err
	}
	if current.HeadSHA != p.HeadSHA || current.SourceSHA256 != p.SourceSHA256 || current.AfterSHA256 != p.AfterSHA256 || current.EvidenceID != p.EvidenceID || current.JobIndex != p.JobIndex || current.StepIndex != p.StepIndex || current.JobName != p.JobName || current.StepName != p.StepName || current.Line != p.Line || current.WaitSeconds != p.WaitSeconds || current.Runs != p.Runs || current.WaitMS != p.WaitMS || current.Condition != p.Condition || current.Edit != p.Edit || current.File != p.File {
		return ErrReadinessUnsupported
	}
	return nil
}

// shellQuote wraps one string in single quotes for sh, escaping an embedded
// quote in the only way a single-quoted shell word allows.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
