package optimisation

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"strconv"

	"github.com/eyupio/zoomies/internal/kennel/workflow"
	"github.com/eyupio/zoomies/internal/prrepair"
)

// ErrTimeoutUnsupported is the one refusal for a Kennel timeout fix. It names
// the shape the fix needs without guessing the minutes.
var ErrTimeoutUnsupported = errors.New("timeout fix needs an executable job with no timeout-minutes and a one-line runs-on, the workflow file the Kennel finding names, minutes supplied by the operator and source/edit policy")

// TimeoutCode is the Kennel check this fix closes.
const TimeoutCode = "ci.no_timeout"

// GitBlobSHA is the name Git gives the content, which is how a Kennel finding
// names the file it was found in. A fix is only previewed against content that
// has that name. SHA-1 is Git's choice and this is an identity check, not a
// security boundary: the content's own SHA-256 is what is bound and compared.
func GitBlobSHA(content string) string {
	h := sha1.New()
	h.Write([]byte("blob " + strconv.Itoa(len(content)) + "\x00"))
	h.Write([]byte(content))
	return hex.EncodeToString(h.Sum(nil))
}

// TimeoutSource is the workflow file as read at one commit.
type TimeoutSource struct {
	HeadSHA  string
	Workflow prrepair.File
}

// TimeoutRequest says which finding the fix is for. The blob and the job index
// come from the Kennel finding's evidence; the minutes come from the operator,
// because a timeout is a fact about how long the job should take, and nothing
// in the file says that.
type TimeoutRequest struct {
	BlobSHA  string
	JobIndex int
	Minutes  int
}

// TimeoutPreview is a proposed addition of one timeout-minutes to one job.
// Verification is always a Kennel recheck: the finding closes when the change
// reaches the branch Kennel reads, and not because a pull request was opened.
type TimeoutPreview struct {
	Version      string
	Kind         string
	State        string
	CheckCode    string
	HeadSHA      string
	BlobSHA      string
	SourceSHA256 string
	AfterSHA256  string
	JobIndex     int
	JobName      string
	Line         int
	Minutes      int
	Edit         prrepair.Edit
	File         prrepair.File
	Verification string
	Explanation  string
}

// PreviewTimeout computes the exact insertion. It reads nothing and calls no
// model; the caller has already read the file and holds the policy.
func PreviewTimeout(source TimeoutSource, req TimeoutRequest, policy Policy) (TimeoutPreview, error) {
	if !policy.SourceAllowed || !policy.WorkflowEditsAllowed || !commit(source.HeadSHA) || !cacheWorkflow(source.Workflow) {
		return TimeoutPreview{}, ErrTimeoutUnsupported
	}
	if req.Minutes < 1 || req.Minutes > workflow.MaxTimeoutMinutes || req.JobIndex < 0 || req.BlobSHA != GitBlobSHA(source.Workflow.Content) {
		return TimeoutPreview{}, ErrTimeoutUnsupported
	}
	data := []byte(source.Workflow.Content)
	jobName, err := workflow.TimeoutJobName(data, req.JobIndex)
	if err != nil {
		return TimeoutPreview{}, ErrTimeoutUnsupported
	}
	before, after, line, err := workflow.TimeoutChange(data, req.JobIndex, req.Minutes)
	if err != nil {
		return TimeoutPreview{}, ErrTimeoutUnsupported
	}
	edit := prrepair.Edit{Path: source.Workflow.Path, Before: before, After: after}
	files, err := prrepair.ApplySuppliedEdits([]prrepair.File{source.Workflow}, []prrepair.Edit{edit}, true)
	if err != nil {
		return TimeoutPreview{}, ErrTimeoutUnsupported
	}
	return TimeoutPreview{
		Version: "timeout-preview-v1", Kind: "kennel_fix_timeout", State: "preview_ready", CheckCode: TimeoutCode,
		HeadSHA: source.HeadSHA, BlobSHA: req.BlobSHA, SourceSHA256: hash(source.Workflow.Content), AfterSHA256: hash(files[0].Content),
		JobIndex: req.JobIndex, JobName: jobName, Line: line, Minutes: req.Minutes, Edit: edit, File: files[0],
		Verification: "recheck",
		Explanation:  "Give " + jobName + " a timeout of " + strconv.Itoa(req.Minutes) + " minutes so a hung job cannot hold a runner until the platform limit. The minutes are the operator's, not inferred: too small a value fails a healthy job. The finding closes when Kennel Club next reads this file on the branch and no longer sees the job without one, not when a pull request is opened.",
	}, nil
}

// CheckTimeoutCurrent is a local precondition, not publication authorisation:
// it recomputes the preview from the source now and refuses any difference.
func CheckTimeoutCurrent(p TimeoutPreview, source TimeoutSource, req TimeoutRequest, policy Policy) error {
	if p.Version != "timeout-preview-v1" || p.Kind != "kennel_fix_timeout" || p.State != "preview_ready" {
		return ErrTimeoutUnsupported
	}
	current, err := PreviewTimeout(source, req, policy)
	if err != nil {
		return err
	}
	if current != p {
		return ErrTimeoutUnsupported
	}
	return nil
}
