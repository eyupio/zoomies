package github

import (
	"context"
	"encoding/base64"
	"errors"
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

const SetupCacheEvidenceMaximumRequests = 8

var (
	ErrSetupCacheNotFound  = errors.New("no supported root lockfile was found")
	ErrSetupCacheAmbiguous = errors.New("more than one supported root lockfile was found")
	ErrSetupCacheChanged   = errors.New("the setup cache evidence changed during collection")
)

// SetupCacheSelection names one workflow attempt. The selected job and its
// retained history are matched locally; this read only needs the attempt's
// pinned workflow source and lockfile.
type SetupCacheSelection struct {
	RunID   int64
	Attempt int
}

func ValidSetupCacheSelection(s SetupCacheSelection) bool {
	return s.RunID > 0 && s.Attempt > 0 && s.Attempt <= 1000
}

// SetupCacheSource is the bounded source evidence a cache preview needs. The
// workflow and lockfile are regular files at the attempt's head commit; the
// literal job and setup step names let the controller match the fleet's job row.
type SetupCacheSource struct {
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
	LockfilePath    string
	Lockfile        prrepair.File
	LockfileBlobSHA string
	JobIndex        int
	StepIndex       int
	JobName         string
	StepName        string
}

type SetupCacheSourceReader interface {
	ReadSetupCacheSource(context.Context, string, SetupCacheSelection) (*SetupCacheSource, error)
}

var _ SetupCacheSourceReader = (*appClient)(nil)

// ReadSetupCacheSource uses the pinned attempt, a bounded commit tree and the
// two root files. It follows no redirects, reads no history from GitHub and
// verifies the attempt and each file again before returning.
func (c *appClient) ReadSetupCacheSource(ctx context.Context, repo string, selected SetupCacheSelection) (*SetupCacheSource, error) {
	owner, name, err := splitRepo(repo)
	if err != nil || !ValidSetupCacheSelection(selected) {
		return nil, fmt.Errorf("github: select a valid cache preview run and attempt")
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
			return nil, b.fail("read cache preview workflow attempt", resp, err)
		}
		if r == nil || r.GetID() != selected.RunID || r.GetRunAttempt() != selected.Attempt || r.GetRepository().GetFullName() != repo || r.GetStatus() != "completed" || r.GetConclusion() != "success" || r.GetEvent() != "push" || !validDelaySHA(r.GetHeadSHA()) || experimentSourcePath(&ExperimentAttemptEvidence{Event: r.GetEvent(), HeadSHA: r.GetHeadSHA(), WorkflowPath: r.GetPath()}) == "" {
			return nil, fmt.Errorf("github: cache preview attempt has unsupported or incomplete identity")
		}
		return r, nil
	}
	initial, err := readRun()
	if err != nil {
		return nil, err
	}
	tree, resp, err := b.asInstallation.Git.GetTree(ctx, owner, name, initial.GetHeadSHA(), true)
	if err != nil {
		return nil, b.fail("read cache preview commit tree", resp, err)
	}
	if tree == nil || tree.GetTruncated() || len(tree.Entries) > 10000 {
		return nil, fmt.Errorf("github: cache preview commit tree is incomplete or too large")
	}
	type entry struct {
		mode, sha string
		size      int
	}
	entries := make(map[string]entry, len(tree.Entries))
	for _, e := range tree.Entries {
		if e.GetType() == "blob" && (e.GetMode() == "100644" || e.GetMode() == "100755") && e.GetSize() > 0 && validDelaySHA(strings.ToLower(e.GetSHA())) {
			entries[e.GetPath()] = entry{e.GetMode(), strings.ToLower(e.GetSHA()), e.GetSize()}
		}
	}
	workflowPath := initial.GetPath()
	workflowEntry, ok := entries[workflowPath]
	if !ok || workflowEntry.size > optimisation.MaxCheckoutSourceBytes {
		return nil, fmt.Errorf("github: cache preview workflow is not a supported regular root file")
	}
	readFile := func(path string, want entry, limit int) (prrepair.File, error) {
		f, directory, resp, err := b.asInstallation.Repositories.GetContents(ctx, owner, name, path, &gh.RepositoryContentGetOptions{Ref: initial.GetHeadSHA()})
		if err != nil {
			return prrepair.File{}, b.fail("read cache preview "+path, resp, err)
		}
		raw := ""
		if f != nil && f.Content != nil {
			raw = *f.Content
		}
		if f == nil || directory != nil || f.GetType() != "file" || f.GetPath() != path || f.GetEncoding() != "base64" || f.GetSize() <= 0 || f.GetSize() > limit || len(raw) > limit*4/3+4096 || f.GetSubmoduleGitURL() != "" || f.GetTarget() != "" {
			return prrepair.File{}, fmt.Errorf("github: cache preview file has invalid identity, type, size or encoding")
		}
		content, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(raw, "\n", ""))
		if err != nil || len(content) != f.GetSize() || !utf8.Valid(content) || strings.ToLower(f.GetSHA()) != want.sha || contextGitBlobSHA(string(content)) != want.sha {
			return prrepair.File{}, fmt.Errorf("github: cache preview file failed content integrity validation")
		}
		return prrepair.File{Path: path, Mode: want.mode, Content: string(content)}, nil
	}
	workflowFile, err := readFile(workflowPath, workflowEntry, optimisation.MaxCheckoutSourceBytes)
	if err != nil {
		return nil, err
	}
	var lockfilePath string
	for _, candidate := range []string{"package-lock.json", "npm-shrinkwrap.json", "yarn.lock", "pnpm-lock.yaml"} {
		if _, ok := entries[candidate]; !ok {
			continue
		}
		if lockfilePath != "" {
			return nil, ErrSetupCacheAmbiguous
		}
		lockfilePath = candidate
	}
	if lockfilePath == "" {
		return nil, ErrSetupCacheNotFound
	}
	lockfileEntry := entries[lockfilePath]
	if lockfileEntry.size > optimisation.MaxLockfileBytes {
		return nil, fmt.Errorf("github: cache preview lockfile is too large")
	}
	lockfileFile, err := readFile(lockfilePath, lockfileEntry, optimisation.MaxLockfileBytes)
	if err != nil {
		return nil, err
	}
	job, step, err := workflow.FindSetupNode([]byte(workflowFile.Content))
	if err != nil {
		return nil, fmt.Errorf("github: cache preview setup step is missing or ambiguous")
	}
	jobName, stepName, err := workflow.SetupNames([]byte(workflowFile.Content), job, step)
	if err != nil {
		return nil, fmt.Errorf("github: cache preview setup step needs literal job and step names")
	}
	final, err := readRun()
	if err != nil {
		return nil, err
	}
	if final.GetHeadSHA() != initial.GetHeadSHA() || final.GetPath() != initial.GetPath() || final.GetStatus() != initial.GetStatus() || final.GetConclusion() != initial.GetConclusion() || final.GetEvent() != initial.GetEvent() {
		return nil, ErrSetupCacheChanged
	}
	return &SetupCacheSource{
		Version: "setup-cache-evidence-v1", Basis: "push_head_root_lockfile_literal_names", Repository: repo,
		RunID: strconv.FormatInt(selected.RunID, 10), Attempt: selected.Attempt, HeadSHA: initial.GetHeadSHA(), HeadBranch: initial.GetHeadBranch(),
		WorkflowPath: workflowPath, Workflow: workflowFile, WorkflowBlobSHA: workflowEntry.sha,
		LockfilePath: lockfilePath, Lockfile: lockfileFile, LockfileBlobSHA: lockfileEntry.sha,
		JobIndex: job, StepIndex: step, JobName: jobName, StepName: stepName,
	}, nil
}

// ValidSetupCacheSource holds a collected source to the same shape the preview
// accepts. It checks bytes and names; it is not an attestation of executed code.
func ValidSetupCacheSource(s *SetupCacheSource, repo string, selection SetupCacheSelection) bool {
	if s == nil || !ValidSetupCacheSelection(selection) || s.Version != "setup-cache-evidence-v1" || s.Basis != "push_head_root_lockfile_literal_names" || s.Repository != repo || s.RunID != strconv.FormatInt(selection.RunID, 10) || s.Attempt != selection.Attempt || !validDelaySHA(s.HeadSHA) || !aicontext.ValidBranch(s.HeadBranch) || experimentSourcePath(&ExperimentAttemptEvidence{Event: "push", HeadSHA: s.HeadSHA, WorkflowPath: s.WorkflowPath}) != s.WorkflowPath || s.Workflow.Path != s.WorkflowPath || s.Workflow.Partial || s.Workflow.StartLine != 0 || len(s.Workflow.Content) == 0 || len(s.Workflow.Content) > optimisation.MaxCheckoutSourceBytes || contextGitBlobSHA(s.Workflow.Content) != s.WorkflowBlobSHA || s.Lockfile.Path != s.LockfilePath || s.Lockfile.Partial || s.Lockfile.StartLine != 0 || len(s.Lockfile.Content) == 0 || len(s.Lockfile.Content) > optimisation.MaxLockfileBytes || !utf8.ValidString(s.Lockfile.Content) || contextGitBlobSHA(s.Lockfile.Content) != s.LockfileBlobSHA || !cacheLockfileSupported(s.LockfilePath) || s.JobName == "" || s.StepName == "" {
		return false
	}
	job, step, err := workflow.FindSetupNode([]byte(s.Workflow.Content))
	if err != nil || job != s.JobIndex || step != s.StepIndex {
		return false
	}
	jobName, stepName, err := workflow.SetupNames([]byte(s.Workflow.Content), job, step)
	return err == nil && jobName == s.JobName && stepName == s.StepName
}

func cacheLockfileSupported(path string) bool {
	switch path {
	case "package-lock.json", "npm-shrinkwrap.json", "yarn.lock", "pnpm-lock.yaml":
		return true
	}
	return false
}
