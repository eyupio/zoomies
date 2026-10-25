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

// ShardEvidenceMaximumRequests bounds the GitHub work for one sharding source
// read: attempt, tree, workflow, final attempt. The shared diagnostic admission
// reserves room for this many before it starts.
const ShardEvidenceMaximumRequests = 4

// ShardSource is the complete root workflow at one completed successful push
// attempt, bound to the literal job and step that run Playwright.
type ShardSource struct {
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
}

type ShardSourceReader interface {
	ReadShardSource(context.Context, string, SetupCacheSelection) (*ShardSource, error)
}

var _ ShardSourceReader = (*appClient)(nil)

// ReadShardSource reads the pinned attempt's root workflow and binds it to the
// one Playwright test step with literal names. A workflow Zoomies would refuse to
// shard is refused here with the reason, before any history is read.
func (c *appClient) ReadShardSource(ctx context.Context, repo string, selected SetupCacheSelection) (*ShardSource, error) {
	var job, step int
	var jobName, stepName string
	root, err := c.readRootWorkflow(ctx, repo, selected, "sharding preview", func(file prrepair.File) error {
		var err error
		job, step, err = workflow.FindPlaywrightTest([]byte(file.Content))
		if err != nil {
			return fmt.Errorf("github: sharding preview needs one shardable Playwright step: %w", err)
		}
		jobName, stepName, err = workflow.ShardNames([]byte(file.Content), job, step)
		if err != nil {
			return fmt.Errorf("github: sharding preview step needs literal job and step names")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &ShardSource{
		Version: "shard-evidence-v1", Basis: "push_head_root_workflow_literal_names", Repository: repo,
		RunID: strconv.FormatInt(selected.RunID, 10), Attempt: selected.Attempt, HeadSHA: root.HeadSHA, HeadBranch: root.HeadBranch,
		WorkflowPath: root.Path, Workflow: root.File, WorkflowBlobSHA: root.BlobSHA, WorkflowSHA256: contentSHA256(root.File.Content),
		JobIndex: job, StepIndex: step, JobName: jobName, StepName: stepName,
	}, nil
}

// ValidShardSource holds a collected source to the same shape the preview
// accepts. It checks bytes and names; it is not an attestation of executed code.
func ValidShardSource(s *ShardSource, repo string, selection SetupCacheSelection) bool {
	if s == nil || !ValidSetupCacheSelection(selection) || s.Version != "shard-evidence-v1" || s.Basis != "push_head_root_workflow_literal_names" || s.Repository != repo || s.RunID != strconv.FormatInt(selection.RunID, 10) || s.Attempt != selection.Attempt || !validDelaySHA(s.HeadSHA) || !aicontext.ValidBranch(s.HeadBranch) || experimentSourcePath(&ExperimentAttemptEvidence{Event: "push", HeadSHA: s.HeadSHA, WorkflowPath: s.WorkflowPath}) != s.WorkflowPath || s.Workflow.Path != s.WorkflowPath || s.Workflow.Partial || s.Workflow.StartLine != 0 || len(s.Workflow.Content) == 0 || len(s.Workflow.Content) > optimisation.MaxCheckoutSourceBytes || contextGitBlobSHA(s.Workflow.Content) != s.WorkflowBlobSHA || contentSHA256(s.Workflow.Content) != s.WorkflowSHA256 || s.JobName == "" || s.StepName == "" {
		return false
	}
	job, step, err := workflow.FindPlaywrightTest([]byte(s.Workflow.Content))
	if err != nil || job != s.JobIndex || step != s.StepIndex {
		return false
	}
	jobName, stepName, err := workflow.ShardNames([]byte(s.Workflow.Content), job, step)
	return err == nil && jobName == s.JobName && stepName == s.StepName
}
