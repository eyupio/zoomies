package github

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/eyupio/zoomies/internal/aicontext"
	"github.com/eyupio/zoomies/internal/optimisation"
	"github.com/eyupio/zoomies/internal/prrepair"
	gh "github.com/google/go-github/v88/github"
)

// KennelFixMaximumRequests bounds the GitHub work for one Kennel fix source
// read: the repository, the branch, its tree, the file, and the branch again.
// The shared diagnostic admission reserves room for this many before it starts.
const KennelFixMaximumRequests = 5

// ErrKennelFixStale means the default branch no longer holds the file the
// finding names. Kennel Club will read it again and the finding may be gone or
// new; a fix is made again from the finding, never applied to other bytes.
var ErrKennelFixStale = errors.New("github: the default branch no longer holds the workflow file the finding names")

// KennelFixSource is one workflow file of the default branch, read at the
// branch's head commit and checked to be exactly the blob a Kennel finding
// names. Its content is the only thing a fix is computed from.
type KennelFixSource struct {
	Version        string
	Repository     string
	Branch         string
	HeadSHA        string
	Path           string
	BlobSHA        string
	Workflow       prrepair.File
	WorkflowSHA256 string
}

// KennelFixSourceReader reads the source for a Kennel fix. It is separate from
// the Client interface, like the other optimisation readers.
type KennelFixSourceReader interface {
	ReadKennelFixSource(ctx context.Context, repo, path, blobSHA string) (*KennelFixSource, error)
}

var _ KennelFixSourceReader = (*appClient)(nil)

// ReadKennelFixSource reads the default branch's head, finds path in its tree
// and requires the entry to be the blob the finding names, reads that file and
// checks its content against the blob's name, then reads the branch head again
// so the file is bound to a head that did not move while it was read. It
// follows no redirect, reads no history and no other file.
func (c *appClient) ReadKennelFixSource(ctx context.Context, repo, workflowPath, blobSHA string) (*KennelFixSource, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return nil, err
	}
	if !workflowSHA.MatchString(blobSHA) || blobSHA != strings.ToLower(blobSHA) || experimentSourcePath(&ExperimentAttemptEvidence{Event: "push", HeadSHA: strings.Repeat("a", 40), WorkflowPath: workflowPath}) != workflowPath {
		return nil, fmt.Errorf("github: select a root workflow file and the blob a finding names")
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
	r, resp, err := b.asInstallation.Repositories.Get(ctx, owner, name)
	if err != nil {
		return nil, b.fail("read Kennel fix repository", resp, err)
	}
	branch := r.GetDefaultBranch()
	if r.GetFullName() != repo || !aicontext.ValidBranch(branch) || r.GetArchived() {
		return nil, fmt.Errorf("github: Kennel fix repository has no usable default branch")
	}
	readHead := func() (string, error) {
		br, resp, err := b.asInstallation.Repositories.GetBranch(ctx, owner, name, branch, 0)
		if err != nil {
			return "", b.fail("read Kennel fix branch", resp, err)
		}
		sha := br.GetCommit().GetSHA()
		if !validDelaySHA(sha) {
			return "", fmt.Errorf("github: Kennel fix branch has no commit identity")
		}
		return sha, nil
	}
	head, err := readHead()
	if err != nil {
		return nil, err
	}
	tree, resp, err := b.asInstallation.Git.GetTree(ctx, owner, name, head, true)
	if err != nil {
		return nil, b.fail("read Kennel fix commit tree", resp, err)
	}
	if tree == nil || tree.GetTruncated() || len(tree.Entries) > 10000 {
		return nil, fmt.Errorf("github: Kennel fix commit tree is incomplete or too large")
	}
	var entry *gh.TreeEntry
	for _, e := range tree.Entries {
		if e.GetPath() == workflowPath {
			entry = e
			break
		}
	}
	if entry == nil || !strings.EqualFold(entry.GetSHA(), blobSHA) {
		return nil, ErrKennelFixStale
	}
	if entry.GetType() != "blob" || (entry.GetMode() != "100644" && entry.GetMode() != "100755") || entry.GetSize() <= 0 || entry.GetSize() > optimisation.MaxCheckoutSourceBytes {
		return nil, fmt.Errorf("github: Kennel fix workflow is not a supported regular root file")
	}
	f, directory, resp, err := b.asInstallation.Repositories.GetContents(ctx, owner, name, workflowPath, &gh.RepositoryContentGetOptions{Ref: head})
	if err != nil {
		return nil, b.fail("read Kennel fix "+workflowPath, resp, err)
	}
	raw := ""
	if f != nil && f.Content != nil {
		raw = *f.Content
	}
	if f == nil || directory != nil || f.GetType() != "file" || f.GetPath() != workflowPath || f.GetEncoding() != "base64" || f.GetSize() <= 0 || f.GetSize() > optimisation.MaxCheckoutSourceBytes || len(raw) > optimisation.MaxCheckoutSourceBytes*4/3+4096 || f.GetSubmoduleGitURL() != "" || f.GetTarget() != "" {
		return nil, fmt.Errorf("github: Kennel fix file has invalid identity, type, size or encoding")
	}
	content, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(raw, "\n", ""))
	if err != nil || len(content) != f.GetSize() || !utf8.Valid(content) || !strings.EqualFold(f.GetSHA(), blobSHA) || contextGitBlobSHA(string(content)) != blobSHA {
		return nil, fmt.Errorf("github: Kennel fix file failed content integrity validation")
	}
	after, err := readHead()
	if err != nil {
		return nil, err
	}
	if after != head {
		return nil, ErrSetupCacheChanged
	}
	file := prrepair.File{Path: workflowPath, Mode: entry.GetMode(), Content: string(content)}
	return &KennelFixSource{
		Version: "kennel-fix-source-v1", Repository: repo, Branch: branch, HeadSHA: head, Path: workflowPath, BlobSHA: blobSHA,
		Workflow: file, WorkflowSHA256: contentSHA256(file.Content),
	}, nil
}

// ValidKennelFixSource holds a collected source to the shape the preview
// accepts. It checks bytes and names; it is not an attestation of executed code.
func ValidKennelFixSource(s *KennelFixSource, repo, workflowPath, blobSHA string) bool {
	return s != nil && s.Version == "kennel-fix-source-v1" && s.Repository == repo && s.Path == workflowPath && s.BlobSHA == blobSHA &&
		aicontext.ValidBranch(s.Branch) && validDelaySHA(s.HeadSHA) && s.Workflow.Path == workflowPath && !s.Workflow.Partial && s.Workflow.StartLine == 0 &&
		(s.Workflow.Mode == "100644" || s.Workflow.Mode == "100755") && len(s.Workflow.Content) > 0 && len(s.Workflow.Content) <= optimisation.MaxCheckoutSourceBytes &&
		contextGitBlobSHA(s.Workflow.Content) == blobSHA && contentSHA256(s.Workflow.Content) == s.WorkflowSHA256
}
