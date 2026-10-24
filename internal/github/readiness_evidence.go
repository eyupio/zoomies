package github

import (
	"context"
	"encoding/base64"
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

// ReadinessEvidenceMaximumRequests bounds the GitHub work for one readiness
// source read: attempt, tree, workflow, final attempt. The shared diagnostic
// admission reserves room for this many before it starts.
const ReadinessEvidenceMaximumRequests = 4

// ReadinessSource is the complete root workflow at one completed successful push
// attempt, bound to the literal job and step that carry the fixed sleep.
type ReadinessSource struct {
	Version         string
	Basis           string
	Repository      string
	RunID           string
	Attempt         int
	HeadSHA         string
	HeadBranch      string
	WorkflowPath    string
	Workflow        prrepair.File
	WorkflowBlobSHA string
	WorkflowSHA256  string
	JobIndex        int
	StepIndex       int
	JobName         string
	StepName        string
	WaitSeconds     int
}

type ReadinessSourceReader interface {
	ReadReadinessSource(context.Context, string, SetupCacheSelection) (*ReadinessSource, error)
}

var _ ReadinessSourceReader = (*appClient)(nil)

// ReadReadinessSource uses the pinned attempt, a bounded commit tree and the
// root workflow. It reads no history, no lockfile and no redirect, and verifies
// the attempt and file again before returning.
func (c *appClient) ReadReadinessSource(ctx context.Context, repo string, selected SetupCacheSelection) (*ReadinessSource, error) {
	owner, name, err := splitRepo(repo)
	if err != nil || !ValidSetupCacheSelection(selected) {
		return nil, fmt.Errorf("github: select a valid readiness preview run and attempt")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	b, err := c.prDelayClient()
	if err != nil {
		return nil, err
	}
	hc := *b.asInstallation.Client()
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	b.asInstallation, err = newGitHubClient(&hc, b.asInstallation.BaseURL(), b.asInstallation.UploadURL())
	if err != nil {
		return nil, err
	}
	readRun := func() (*gh.WorkflowRun, error) {
		r, resp, err := b.asInstallation.Actions.GetWorkflowRunAttempt(ctx, owner, name, selected.RunID, selected.Attempt, nil)
		if err != nil {
			return nil, b.fail("read readiness preview workflow attempt", resp, err)
		}
		if r == nil || r.GetID() != selected.RunID || r.GetRunAttempt() != selected.Attempt || r.GetRepository().GetFullName() != repo || r.GetStatus() != "completed" || r.GetConclusion() != "success" || r.GetEvent() != "push" || !validDelaySHA(r.GetHeadSHA()) || experimentSourcePath(&ExperimentAttemptEvidence{Event: r.GetEvent(), HeadSHA: r.GetHeadSHA(), WorkflowPath: r.GetPath()}) == "" {
			return nil, fmt.Errorf("github: readiness preview attempt has unsupported or incomplete identity")
		}
		return r, nil
	}
	initial, err := readRun()
	if err != nil {
		return nil, err
	}
	tree, resp, err := b.asInstallation.Git.GetTree(ctx, owner, name, initial.GetHeadSHA(), true)
	if err != nil {
		return nil, b.fail("read readiness preview commit tree", resp, err)
	}
	if tree == nil || tree.GetTruncated() || len(tree.Entries) > 10000 {
		return nil, fmt.Errorf("github: readiness preview commit tree is incomplete or too large")
	}
	workflowPath := initial.GetPath()
	var entry *gh.TreeEntry
	for _, e := range tree.Entries {
		if e.GetPath() == workflowPath && e.GetType() == "blob" && (e.GetMode() == "100644" || e.GetMode() == "100755") && e.GetSize() > 0 && e.GetSize() <= optimisation.MaxCheckoutSourceBytes && validDelaySHA(strings.ToLower(e.GetSHA())) {
			entry = e
			break
		}
	}
	if entry == nil {
		return nil, fmt.Errorf("github: readiness preview workflow is not a supported regular root file")
	}
	f, directory, resp, err := b.asInstallation.Repositories.GetContents(ctx, owner, name, workflowPath, &gh.RepositoryContentGetOptions{Ref: initial.GetHeadSHA()})
	if err != nil {
		return nil, b.fail("read readiness preview "+workflowPath, resp, err)
	}
	raw := ""
	if f != nil && f.Content != nil {
		raw = *f.Content
	}
	if f == nil || directory != nil || f.GetType() != "file" || f.GetPath() != workflowPath || f.GetEncoding() != "base64" || f.GetSize() <= 0 || f.GetSize() > optimisation.MaxCheckoutSourceBytes || len(raw) > optimisation.MaxCheckoutSourceBytes*4/3+4096 || f.GetSubmoduleGitURL() != "" || f.GetTarget() != "" {
		return nil, fmt.Errorf("github: readiness preview file has invalid identity, type, size or encoding")
	}
	content, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(raw, "\n", ""))
	if err != nil || len(content) != f.GetSize() || !utf8.Valid(content) || !strings.EqualFold(f.GetSHA(), entry.GetSHA()) || contextGitBlobSHA(string(content)) != strings.ToLower(entry.GetSHA()) {
		return nil, fmt.Errorf("github: readiness preview file failed content integrity validation")
	}
	workflowFile := prrepair.File{Path: workflowPath, Mode: entry.GetMode(), Content: string(content)}
	job, step, seconds, err := workflow.FindFixedSleep([]byte(workflowFile.Content))
	if err != nil {
		return nil, fmt.Errorf("github: readiness preview fixed sleep step is missing or ambiguous")
	}
	jobName, stepName, err := workflow.ReadinessNames([]byte(workflowFile.Content), job, step)
	if err != nil {
		return nil, fmt.Errorf("github: readiness preview step needs literal job and step names")
	}
	final, err := readRun()
	if err != nil {
		return nil, err
	}
	if final.GetHeadSHA() != initial.GetHeadSHA() || final.GetPath() != initial.GetPath() || final.GetStatus() != initial.GetStatus() || final.GetConclusion() != initial.GetConclusion() || final.GetEvent() != initial.GetEvent() {
		return nil, ErrSetupCacheChanged
	}
	return &ReadinessSource{
		Version: "readiness-evidence-v1", Basis: "push_head_root_workflow_literal_names", Repository: repo,
		RunID: strconv.FormatInt(selected.RunID, 10), Attempt: selected.Attempt, HeadSHA: initial.GetHeadSHA(), HeadBranch: initial.GetHeadBranch(),
		WorkflowPath: workflowPath, Workflow: workflowFile, WorkflowBlobSHA: strings.ToLower(entry.GetSHA()), WorkflowSHA256: contentSHA256(workflowFile.Content),
		JobIndex: job, StepIndex: step, JobName: jobName, StepName: stepName, WaitSeconds: seconds,
	}, nil
}

// ValidReadinessSource holds a collected source to the same shape the preview
// accepts. It checks bytes and names; it is not an attestation of executed code.
func ValidReadinessSource(s *ReadinessSource, repo string, selection SetupCacheSelection) bool {
	if s == nil || !ValidSetupCacheSelection(selection) || s.Version != "readiness-evidence-v1" || s.Basis != "push_head_root_workflow_literal_names" || s.Repository != repo || s.RunID != strconv.FormatInt(selection.RunID, 10) || s.Attempt != selection.Attempt || !validDelaySHA(s.HeadSHA) || !aicontext.ValidBranch(s.HeadBranch) || experimentSourcePath(&ExperimentAttemptEvidence{Event: "push", HeadSHA: s.HeadSHA, WorkflowPath: s.WorkflowPath}) != s.WorkflowPath || s.Workflow.Path != s.WorkflowPath || s.Workflow.Partial || s.Workflow.StartLine != 0 || len(s.Workflow.Content) == 0 || len(s.Workflow.Content) > optimisation.MaxCheckoutSourceBytes || contextGitBlobSHA(s.Workflow.Content) != s.WorkflowBlobSHA || contentSHA256(s.Workflow.Content) != s.WorkflowSHA256 || s.JobName == "" || s.StepName == "" || s.WaitSeconds < 2 || s.WaitSeconds > 3600 {
		return false
	}
	job, step, seconds, err := workflow.FindFixedSleep([]byte(s.Workflow.Content))
	if err != nil || job != s.JobIndex || step != s.StepIndex || seconds != s.WaitSeconds {
		return false
	}
	jobName, stepName, err := workflow.ReadinessNames([]byte(s.Workflow.Content), job, step)
	return err == nil && jobName == s.JobName && stepName == s.StepName
}
