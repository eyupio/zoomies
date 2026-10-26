package github

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/eyupio/zoomies/internal/kennel/offline"
	"github.com/eyupio/zoomies/internal/kennel/workflow"
	gh "github.com/google/go-github/v88/github"
)

// KennelReviewMaximumRequests bounds the GitHub work for one pull request
// review: the pull request twice, the merge base, up to three pages of changed
// files, and a base and a head read for each of at most MaxReviewFiles files.
const KennelReviewMaximumRequests = 2 + 1 + 3 + 2*offline.MaxReviewFiles

const kennelReviewFilePages = 3

// KennelReviewSource is a pull request's change, read at two pinned commits.
// BaseSHA is the merge base, which is what GitHub's own file list is a diff
// against, so a change made on the base branch since is not blamed on the pull
// request.
type KennelReviewSource struct {
	Version    string
	Repository string
	Number     int
	Open       bool
	BaseBranch string
	BaseSHA    string
	HeadSHA    string
	// Changed is every path the pull request changes, at its head name.
	Changed []string
	// FilesCut is true when the pull request changes more files than GitHub's
	// list returned here, so Changed is the first of them.
	FilesCut   bool
	Base, Head []offline.ReviewFile
	Unreadable []string
}

// KennelReviewSourceReader reads a pull request for a review. It is separate
// from Client, like the other source readers.
type KennelReviewSourceReader interface {
	ReadKennelReviewSource(ctx context.Context, repo string, number int) (*KennelReviewSource, error)
}

var _ KennelReviewSourceReader = (*appClient)(nil)

// ReadKennelReviewSource pins the pull request's head, finds the merge base,
// lists the changed files, and reads the changed root workflow files at the
// merge base and at the head, then reads the pull request again so a push that
// landed meanwhile fails the read rather than mixing two heads. It reads no
// file outside .github/workflows and runs nothing.
func (c *appClient) ReadKennelReviewSource(ctx context.Context, repo string, number int) (*KennelReviewSource, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return nil, err
	}
	if number < 1 {
		return nil, fmt.Errorf("github: a pull request number must be positive")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
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
	readPull := func() (*gh.PullRequest, error) {
		p, resp, err := b.asInstallation.PullRequests.Get(ctx, owner, name, number)
		if err != nil {
			return nil, b.fail("read pull request for review", resp, err)
		}
		if p.GetNumber() != number || !validDelaySHA(p.GetHead().GetSHA()) || !validDelaySHA(p.GetBase().GetSHA()) ||
			p.GetBase().GetRepo().GetFullName() != repo || p.GetBase().GetRef() == "" || len(p.GetBase().GetRef()) > 1024 || !utf8.ValidString(p.GetBase().GetRef()) {
			return nil, fmt.Errorf("github: the pull request has incomplete or inconsistent identity")
		}
		return p, nil
	}
	pull, err := readPull()
	if err != nil {
		return nil, err
	}
	head := pull.GetHead().GetSHA()
	cmp, resp, err := b.asInstallation.Repositories.CompareCommits(ctx, owner, name, pull.GetBase().GetSHA(), head, &gh.ListOptions{PerPage: 1})
	if err != nil {
		return nil, b.fail("find the pull request's merge base", resp, err)
	}
	mergeBase := cmp.GetMergeBaseCommit().GetSHA()
	if !validDelaySHA(mergeBase) {
		return nil, fmt.Errorf("github: the pull request has no merge base")
	}
	out := &KennelReviewSource{Version: "kennel-review-source-v1", Repository: repo, Number: number, Open: pull.GetState() == "open",
		BaseBranch: pull.GetBase().GetRef(), BaseSHA: mergeBase, HeadSHA: head, Changed: []string{}, Base: []offline.ReviewFile{}, Head: []offline.ReviewFile{}, Unreadable: []string{}}

	type change struct{ path, previous, status string }
	var changes []change
	for page := 1; page <= kennelReviewFilePages; page++ {
		files, resp, err := b.asInstallation.PullRequests.ListFiles(ctx, owner, name, number, &gh.ListOptions{Page: page, PerPage: 100})
		if err != nil {
			return nil, b.fail("list the pull request's files", resp, err)
		}
		for _, f := range files {
			changes = append(changes, change{f.GetFilename(), f.GetPreviousFilename(), f.GetStatus()})
		}
		if resp == nil || resp.NextPage == 0 {
			break
		}
		if page == kennelReviewFilePages {
			out.FilesCut = true
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].path < changes[j].path })
	var selected []change
	for _, ch := range changes {
		out.Changed = append(out.Changed, ch.path)
		if offline.GatePath(ch.path) != "" && len(selected) < offline.MaxReviewFiles {
			selected = append(selected, ch)
		}
	}
	read := func(path, sha string) ([]byte, bool, error) {
		f, dir, resp, err := b.asInstallation.Repositories.GetContents(ctx, owner, name, path, &gh.RepositoryContentGetOptions{Ref: sha})
		if err != nil {
			if resp != nil && resp.StatusCode == http.StatusNotFound {
				return nil, false, nil
			}
			return nil, false, b.fail("read "+path+" for review", resp, err)
		}
		// A file GitHub will not return whole (over a megabyte) or one that is
		// not a plain file is unreadable here, and said to be.
		if f == nil || dir != nil || f.GetType() != "file" || f.GetEncoding() != "base64" || f.GetSize() > workflow.MaxBytes || f.GetSubmoduleGitURL() != "" || f.GetTarget() != "" {
			return nil, true, nil
		}
		encoded := ""
		if f.Content != nil {
			encoded = *f.Content
		}
		raw, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(encoded, "\n", ""))
		if err != nil || len(raw) != f.GetSize() || !utf8.Valid(raw) || contextGitBlobSHA(string(raw)) != strings.ToLower(f.GetSHA()) {
			return nil, true, nil
		}
		return raw, false, nil
	}
	for _, ch := range selected {
		unreadable := false
		if ch.status != "removed" {
			data, bad, err := read(ch.path, head)
			if err != nil {
				return nil, err
			}
			// A file the list says exists at the head and GitHub cannot find is
			// unreadable, not deleted.
			unreadable = unreadable || bad || data == nil
			if data != nil {
				out.Head = append(out.Head, offline.ReviewFile{Path: ch.path, Data: data})
			}
		}
		if ch.status != "added" {
			from := ch.path
			if ch.previous != "" {
				from = ch.previous
			}
			data, bad, err := read(from, mergeBase)
			if err != nil {
				return nil, err
			}
			unreadable = unreadable || bad || data == nil
			if data != nil {
				out.Base = append(out.Base, offline.ReviewFile{Path: ch.path, Data: data})
			}
		}
		if unreadable {
			out.Unreadable = append(out.Unreadable, ch.path)
		}
	}
	after, err := readPull()
	if err != nil {
		return nil, err
	}
	if after.GetHead().GetSHA() != head {
		return nil, ErrSetupCacheChanged
	}
	return out, nil
}
