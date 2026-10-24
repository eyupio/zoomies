package github

import (
	"context"
	"fmt"
	"strconv"

	"github.com/eyupio/zoomies/internal/aicontext"
	"github.com/eyupio/zoomies/internal/kennel/workflow"
	"github.com/eyupio/zoomies/internal/optimisation"
	"github.com/eyupio/zoomies/internal/prrepair"
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

// ReadReadinessSource reads the pinned attempt's root workflow and binds it to
// the one fixed sleep step with literal names. The attempt and file are
// verified again before it returns.
func (c *appClient) ReadReadinessSource(ctx context.Context, repo string, selected SetupCacheSelection) (*ReadinessSource, error) {
	var job, step, seconds int
	var jobName, stepName string
	root, err := c.readRootWorkflow(ctx, repo, selected, "readiness preview", func(file prrepair.File) error {
		var err error
		job, step, seconds, err = workflow.FindFixedSleep([]byte(file.Content))
		if err != nil {
			return fmt.Errorf("github: readiness preview fixed sleep step is missing or ambiguous")
		}
		jobName, stepName, err = workflow.ReadinessNames([]byte(file.Content), job, step)
		if err != nil {
			return fmt.Errorf("github: readiness preview step needs literal job and step names")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &ReadinessSource{
		Version: "readiness-evidence-v1", Basis: "push_head_root_workflow_literal_names", Repository: repo,
		RunID: strconv.FormatInt(selected.RunID, 10), Attempt: selected.Attempt, HeadSHA: root.HeadSHA, HeadBranch: root.HeadBranch,
		WorkflowPath: root.Path, Workflow: root.File, WorkflowBlobSHA: root.BlobSHA, WorkflowSHA256: contentSHA256(root.File.Content),
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
