package github

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/eyupio/zoomies/internal/optimisation"
	"github.com/eyupio/zoomies/internal/prrepair"
	gh "github.com/google/go-github/v88/github"
)

// rootWorkflow is the root workflow file of one completed successful push
// attempt, read at the attempt's own commit.
type rootWorkflow struct {
	HeadSHA    string
	HeadBranch string
	Path       string
	BlobSHA    string
	File       prrepair.File
}

// readRootWorkflow uses the pinned attempt, a bounded commit tree and the root
// workflow, in four requests: the attempt, the tree, the file and the attempt
// again. It reads no history, no lockfile and no redirect. bind runs on the
// verified file before the attempt is read a second time, so what a preview
// found in it is bound to the same unchanged attempt. what names the preview in
// errors ("readiness preview", "sharding preview").
func (c *appClient) readRootWorkflow(ctx context.Context, repo string, selected SetupCacheSelection, what string, bind func(prrepair.File) error) (*rootWorkflow, error) {
	owner, name, err := splitRepo(repo)
	if err != nil || !ValidSetupCacheSelection(selected) {
		return nil, fmt.Errorf("github: select a valid %s run and attempt", what)
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
			return nil, b.fail("read "+what+" workflow attempt", resp, err)
		}
		if r == nil || r.GetID() != selected.RunID || r.GetRunAttempt() != selected.Attempt || r.GetRepository().GetFullName() != repo || r.GetStatus() != "completed" || r.GetConclusion() != "success" || r.GetEvent() != "push" || !validDelaySHA(r.GetHeadSHA()) || experimentSourcePath(&ExperimentAttemptEvidence{Event: r.GetEvent(), HeadSHA: r.GetHeadSHA(), WorkflowPath: r.GetPath()}) == "" {
			return nil, fmt.Errorf("github: %s attempt has unsupported or incomplete identity", what)
		}
		return r, nil
	}
	initial, err := readRun()
	if err != nil {
		return nil, err
	}
	tree, resp, err := b.asInstallation.Git.GetTree(ctx, owner, name, initial.GetHeadSHA(), true)
	if err != nil {
		return nil, b.fail("read "+what+" commit tree", resp, err)
	}
	if tree == nil || tree.GetTruncated() || len(tree.Entries) > 10000 {
		return nil, fmt.Errorf("github: %s commit tree is incomplete or too large", what)
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
		return nil, fmt.Errorf("github: %s workflow is not a supported regular root file", what)
	}
	f, directory, resp, err := b.asInstallation.Repositories.GetContents(ctx, owner, name, workflowPath, &gh.RepositoryContentGetOptions{Ref: initial.GetHeadSHA()})
	if err != nil {
		return nil, b.fail("read "+what+" "+workflowPath, resp, err)
	}
	raw := ""
	if f != nil && f.Content != nil {
		raw = *f.Content
	}
	if f == nil || directory != nil || f.GetType() != "file" || f.GetPath() != workflowPath || f.GetEncoding() != "base64" || f.GetSize() <= 0 || f.GetSize() > optimisation.MaxCheckoutSourceBytes || len(raw) > optimisation.MaxCheckoutSourceBytes*4/3+4096 || f.GetSubmoduleGitURL() != "" || f.GetTarget() != "" {
		return nil, fmt.Errorf("github: %s file has invalid identity, type, size or encoding", what)
	}
	content, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(raw, "\n", ""))
	if err != nil || len(content) != f.GetSize() || !utf8.Valid(content) || !strings.EqualFold(f.GetSHA(), entry.GetSHA()) || contextGitBlobSHA(string(content)) != strings.ToLower(entry.GetSHA()) {
		return nil, fmt.Errorf("github: %s file failed content integrity validation", what)
	}
	file := prrepair.File{Path: workflowPath, Mode: entry.GetMode(), Content: string(content)}
	if err := bind(file); err != nil {
		return nil, err
	}
	final, err := readRun()
	if err != nil {
		return nil, err
	}
	if final.GetHeadSHA() != initial.GetHeadSHA() || final.GetPath() != initial.GetPath() || final.GetStatus() != initial.GetStatus() || final.GetConclusion() != initial.GetConclusion() || final.GetEvent() != initial.GetEvent() {
		return nil, ErrSetupCacheChanged
	}
	return &rootWorkflow{HeadSHA: initial.GetHeadSHA(), HeadBranch: initial.GetHeadBranch(), Path: workflowPath, BlobSHA: strings.ToLower(entry.GetSHA()), File: file}, nil
}
