package github

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/eyupio/zoomies/internal/aicontext"
	"github.com/eyupio/zoomies/internal/kennel/workflow"
	"github.com/eyupio/zoomies/internal/optimisation"
	"github.com/eyupio/zoomies/internal/prrepair"
	gh "github.com/google/go-github/v88/github"
)

const CheckoutEvidenceMaximumRequests = 7

type CheckoutSelection struct {
	RunID       int64
	Attempt     int
	GitHubJobID int64
	JobIndex    int
	StepIndex   int
	// Locate asks the reader to find the one previewable checkout step in the
	// selected job from the source it reads, so nobody counts positions in a
	// file. The positions must be zero when it is set, and the evidence that
	// comes back carries the ones it found with Locate cleared.
	Locate bool
}

// ErrCheckoutNotFound and ErrCheckoutAmbiguous are what a Locate read says when
// the job has no single step that can be previewed.
var (
	ErrCheckoutNotFound  = workflow.ErrCheckoutNotFound
	ErrCheckoutAmbiguous = workflow.ErrCheckoutAmbiguous
)

type CheckoutEvidenceReader interface {
	ReadCheckoutEvidence(context.Context, string, CheckoutSelection) (*CheckoutEvidence, error)
}

var _ CheckoutEvidenceReader = (*appClient)(nil)

// This private source-bearing view associates a push file and uniquely named
// API step. It is descriptive evidence, not an attestation of executed bytes.
type CheckoutEvidence struct {
	Version        string
	Basis          string
	Repository     string
	Selection      CheckoutSelection
	StepNumber     int64
	BlobSHA        string
	JobNameSHA256  string
	StepNameSHA256 string
	Source         optimisation.Source
	Timing         optimisation.Timing
	// HeadBranch is the branch the push ran on, recorded so a later draft pull
	// request targets the branch the source actually came from. Older retained
	// evidence has none and remains readable, but cannot be published.
	HeadBranch string
}

func checkoutHash(s string) string { v := sha256.Sum256([]byte(s)); return hex.EncodeToString(v[:]) }
func checkoutEvidenceID(s CheckoutSelection, number int64) string {
	return fmt.Sprintf("checkout:%d:%d:%d:%d", s.RunID, s.Attempt, s.GitHubJobID, number)
}
func ValidCheckoutSelection(s CheckoutSelection) bool {
	if s.Locate && (s.JobIndex != 0 || s.StepIndex != 0) {
		return false
	}
	return s.RunID > 0 && s.Attempt > 0 && s.GitHubJobID > 0 && s.JobIndex >= 0 && s.JobIndex <= 20000 && s.StepIndex >= 0 && s.StepIndex <= 20000
}
func ValidCheckoutEvidence(e *CheckoutEvidence, repo string, s CheckoutSelection) bool {
	if e == nil || !ValidCheckoutSelection(s) || e.Version != "checkout-evidence-v1" || e.Basis != "push_source_unique_literal_names" || e.Repository != repo || e.Selection != s || e.StepNumber <= 0 || e.StepNumber > 10000 || !validDelaySHA(e.Source.HeadSHA) || e.Source.File.Partial || e.Source.File.StartLine != 0 || len(e.Source.File.Content) > optimisation.MaxCheckoutSourceBytes || contextGitBlobSHA(e.Source.File.Content) != e.BlobSHA || experimentSourcePath(&ExperimentAttemptEvidence{Event: "push", HeadSHA: e.Source.HeadSHA, WorkflowPath: e.Source.File.Path}) == "" || e.HeadBranch != "" && !aicontext.ValidBranch(e.HeadBranch) {
		return false
	}
	job, step, err := workflow.CheckoutNames([]byte(e.Source.File.Content), s.JobIndex, s.StepIndex)
	if err != nil || checkoutHash(job) != e.JobNameSHA256 || checkoutHash(step) != e.StepNameSHA256 {
		return false
	}
	t := e.Timing
	if t.EvidenceID != checkoutEvidenceID(s, e.StepNumber) || t.HeadSHA != e.Source.HeadSHA || t.SourceSHA256 != checkoutHash(e.Source.File.Content) || t.RunID != strconv.FormatInt(s.RunID, 10) || t.Attempt != s.Attempt || t.JobIndex != s.JobIndex || t.StepIndex != s.StepIndex || t.Outcome != "success" || t.CheckoutMS == nil || *t.CheckoutMS <= 0 || *t.CheckoutMS > 7*24*60*60*1000 {
		return false
	}
	before, after, _, err := workflow.CheckoutDepthChange([]byte(e.Source.File.Content), s.JobIndex, s.StepIndex)
	if err != nil {
		return false
	}
	_, err = prrepair.ApplySuppliedEdits([]prrepair.File{e.Source.File}, []prrepair.Edit{{Path: e.Source.File.Path, Before: before, After: after}}, true)
	return err == nil
}

// ReadCheckoutEvidence uses fixed scoped endpoints, isolated body limits and no
// conditional cache. Caller/source-policy admission must precede these reads.
func (c *appClient) ReadCheckoutEvidence(ctx context.Context, repo string, selected CheckoutSelection) (*CheckoutEvidence, error) {
	owner, name, err := splitRepo(repo)
	if err != nil || !ValidCheckoutSelection(selected) {
		return nil, fmt.Errorf("github: select a valid checkout run, attempt, job and source position")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	b, err := c.prDelayClient()
	if err != nil {
		return nil, err
	}
	// A redirect would escape both endpoint scope and the seven-call reservation.
	hc := *b.asInstallation.Client()
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	b.asInstallation, err = newGitHubClient(&hc, b.asInstallation.BaseURL(), b.asInstallation.UploadURL())
	if err != nil {
		return nil, err
	}
	readRun := func() (*gh.WorkflowRun, error) {
		r, resp, err := b.asInstallation.Actions.GetWorkflowRunAttempt(ctx, owner, name, selected.RunID, selected.Attempt, nil)
		if err != nil {
			return nil, b.fail("read checkout workflow attempt", resp, err)
		}
		if r == nil || r.GetID() != selected.RunID || r.GetRunAttempt() != selected.Attempt || r.GetRepository().GetFullName() != repo || r.GetWorkflowID() <= 0 || r.GetStatus() != "completed" || r.GetConclusion() != "success" || r.RunStartedAt == nil || r.UpdatedAt == nil || !r.UpdatedAt.After(r.RunStartedAt.Time) || experimentSourcePath(&ExperimentAttemptEvidence{Event: r.GetEvent(), HeadSHA: r.GetHeadSHA(), WorkflowPath: r.GetPath()}) == "" {
			return nil, fmt.Errorf("github: checkout attempt has unsupported or incomplete identity")
		}
		return r, nil
	}
	initial, err := readRun()
	if err != nil {
		return nil, err
	}
	readJob := func() (*gh.WorkflowJob, error) {
		j, resp, err := b.asInstallation.Actions.GetWorkflowJobByID(ctx, owner, name, selected.GitHubJobID)
		if err != nil {
			return nil, b.fail("read checkout job", resp, err)
		}
		if j == nil || j.GetID() != selected.GitHubJobID || j.GetRunID() != selected.RunID || j.GetRunAttempt() != int64(selected.Attempt) || j.GetHeadSHA() != initial.GetHeadSHA() || j.GetStatus() != "completed" || j.GetConclusion() != "success" || j.StartedAt == nil || j.CompletedAt == nil || j.StartedAt.Before(initial.RunStartedAt.Time) || j.CompletedAt.After(initial.UpdatedAt.Time) || !j.CompletedAt.After(j.StartedAt.Time) || len(j.Steps) < 1 || len(j.Steps) > 200 {
			return nil, fmt.Errorf("github: checkout job does not belong to the successful pinned attempt")
		}
		return j, nil
	}
	job, err := readJob()
	if err != nil {
		return nil, err
	}
	f, dir, resp, err := b.asInstallation.Repositories.GetContents(ctx, owner, name, initial.GetPath(), &gh.RepositoryContentGetOptions{Ref: initial.GetHeadSHA()})
	if err != nil {
		return nil, b.fail("read checkout source at push commit", resp, err)
	}
	raw := ""
	if f != nil && f.Content != nil {
		raw = *f.Content
	}
	if f == nil || dir != nil || f.GetPath() != initial.GetPath() || f.GetType() != "file" || f.GetEncoding() != "base64" || f.GetSize() <= 0 || f.GetSize() > optimisation.MaxCheckoutSourceBytes || f.GetSubmoduleGitURL() != "" || f.GetTarget() != "" || len(raw) > optimisation.MaxCheckoutSourceBytes*4/3+4096 {
		return nil, fmt.Errorf("github: checkout source is unsupported or too large")
	}
	data, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(raw, "\n", ""))
	if err != nil || len(data) != f.GetSize() || !utf8.Valid(data) || contextGitBlobSHA(string(data)) != f.GetSHA() {
		return nil, fmt.Errorf("github: checkout source failed content integrity validation")
	}
	if selected.Locate {
		selected.JobIndex, selected.StepIndex, err = workflow.FindCheckout(data, job.GetName())
		if err != nil {
			return nil, err
		}
		selected.Locate = false
	}
	jobName, stepName, err := workflow.CheckoutNames(data, selected.JobIndex, selected.StepIndex)
	if err != nil || job.GetName() != jobName {
		return nil, fmt.Errorf("github: checkout source cannot be associated with unique literal job and step names")
	}
	var step *gh.TaskStep
	seen := map[int64]bool{}
	for _, item := range job.Steps {
		if item == nil || item.GetNumber() <= 0 || item.GetNumber() > 10000 || seen[item.GetNumber()] {
			return nil, fmt.Errorf("github: checkout step enumeration is ambiguous")
		}
		seen[item.GetNumber()] = true
		if item.GetName() == stepName {
			if step != nil {
				return nil, fmt.Errorf("github: checkout step name is ambiguous")
			}
			step = item
		}
	}
	if step == nil || step.GetStatus() != "completed" || step.GetConclusion() != "success" || step.StartedAt == nil || step.CompletedAt == nil || step.StartedAt.Before(job.StartedAt.Time) || step.CompletedAt.After(job.CompletedAt.Time) || !step.CompletedAt.After(step.StartedAt.Time) {
		return nil, fmt.Errorf("github: checkout step timing is unavailable or inconsistent")
	}
	commit, resp, err := b.asInstallation.Git.GetCommit(ctx, owner, name, initial.GetHeadSHA())
	if err != nil {
		return nil, b.fail("read checkout source commit", resp, err)
	}
	if commit == nil || commit.GetSHA() != initial.GetHeadSHA() || !validDelaySHA(commit.GetTree().GetSHA()) {
		return nil, fmt.Errorf("github: checkout commit has inconsistent identity")
	}
	tree, resp, err := b.asInstallation.Git.GetTree(ctx, owner, name, commit.GetTree().GetSHA(), true)
	if err != nil {
		return nil, b.fail("read checkout source mode", resp, err)
	}
	if tree == nil || tree.GetSHA() != commit.GetTree().GetSHA() || tree.Truncated == nil || tree.GetTruncated() || len(tree.Entries) > 10000 {
		return nil, fmt.Errorf("github: checkout source tree is incomplete")
	}
	mode := ""
	for _, entry := range tree.Entries {
		if entry != nil && entry.GetPath() == f.GetPath() {
			if mode != "" || entry.GetType() != "blob" || entry.GetSHA() != f.GetSHA() || entry.GetMode() != "100644" && entry.GetMode() != "100755" {
				return nil, fmt.Errorf("github: checkout source is not one regular file at the pinned commit")
			}
			mode = entry.GetMode()
		}
	}
	if mode == "" {
		return nil, fmt.Errorf("github: checkout source is absent from its pinned tree")
	}
	final, err := readRun()
	if err != nil {
		return nil, err
	}
	if final.GetHeadSHA() != initial.GetHeadSHA() || final.GetWorkflowID() != initial.GetWorkflowID() || final.GetPath() != initial.GetPath() || final.GetEvent() != initial.GetEvent() || !final.RunStartedAt.Time.Equal(initial.RunStartedAt.Time) || !final.UpdatedAt.Time.Equal(initial.UpdatedAt.Time) {
		return nil, ErrExperimentAttemptChanged
	}
	finalJob, err := readJob()
	if err != nil {
		return nil, err
	}
	// Compare only the observed timing/name identity, not labels, URLs or logs.
	a, err := json.Marshal(struct {
		Name       string
		Start, End *gh.Timestamp
		Steps      []*gh.TaskStep
	}{job.GetName(), job.StartedAt, job.CompletedAt, job.Steps})
	z, finalErr := json.Marshal(struct {
		Name       string
		Start, End *gh.Timestamp
		Steps      []*gh.TaskStep
	}{finalJob.GetName(), finalJob.StartedAt, finalJob.CompletedAt, finalJob.Steps})
	if err != nil || finalErr != nil || string(a) != string(z) {
		return nil, ErrExperimentAttemptChanged
	}
	ms := step.CompletedAt.Sub(step.StartedAt.Time).Milliseconds()
	out := &CheckoutEvidence{Version: "checkout-evidence-v1", Basis: "push_source_unique_literal_names", Repository: repo, Selection: selected, StepNumber: step.GetNumber(), BlobSHA: f.GetSHA(), JobNameSHA256: checkoutHash(jobName), StepNameSHA256: checkoutHash(stepName), HeadBranch: initial.GetHeadBranch(), Source: optimisation.Source{HeadSHA: initial.GetHeadSHA(), File: prrepair.File{Path: f.GetPath(), Mode: mode, Content: string(data)}}, Timing: optimisation.Timing{EvidenceID: checkoutEvidenceID(selected, step.GetNumber()), HeadSHA: initial.GetHeadSHA(), SourceSHA256: checkoutHash(string(data)), RunID: strconv.FormatInt(selected.RunID, 10), Attempt: selected.Attempt, JobIndex: selected.JobIndex, StepIndex: selected.StepIndex, Outcome: "success", CheckoutMS: &ms}}
	if !ValidCheckoutEvidence(out, repo, selected) {
		return nil, fmt.Errorf("github: checkout evidence is inconsistent or unsupported")
	}
	return out, nil
}
