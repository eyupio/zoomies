package controller

import (
	"context"
	"fmt"
	"os"

	"github.com/eyupio/zoomies/internal/gitbare"
	"github.com/eyupio/zoomies/internal/gitfetch"
	"github.com/eyupio/zoomies/internal/github"
)

// mergeLookup is one repository as the merge fetch needs it: the commit read from
// the API and the files from that commit's archive, both through the installation
// the watch was admitted under.
//
// The two halves are deliberately separate calls. The archive says what a commit's
// files are and the commit says what they must hash to, so a reconstruction the
// provider would not recognise is refused before anything is merged.
type mergeLookup struct {
	client github.Client
	repo   string
}

func (l mergeLookup) Commit(ctx context.Context, sha string) (string, []string, error) {
	info, err := l.client.CommitOf(ctx, l.repo, sha)
	if err != nil {
		return "", nil, err
	}
	return info.Tree, info.Parents, nil
}

func (l mergeLookup) Files(ctx context.Context, sha string) ([]gitbare.File, error) {
	// Every provider archive is wrapped in one directory named after the
	// repository and the commit, and that directory is not part of the tree.
	return l.client.ArchiveOf(ctx, l.repo, sha, gitbare.ArchiveOptions{StripTopLevel: true})
}

// mergeRepository builds the repository one conflict repair merges in, and owns
// its lifetime.
//
// The caller's context bounds the whole fetch: every read and both writes happen
// inside it, and a cancelled or timed-out fetch removes what it wrote rather than
// leaving a partial repository behind. Nothing here holds a credential that
// outlives the call, and nothing reads a repository's code.
func mergeRepository(ctx context.Context, client github.Client, repo string, pin gitfetch.Pin) (gitfetch.Repository, error) {
	dir, err := os.MkdirTemp("", "zoomies-merge-")
	if err != nil {
		return gitfetch.Repository{}, fmt.Errorf("preparing the merge workspace: %w", err)
	}
	built, err := gitfetch.Build(ctx, dir, gitfetch.NewSource(mergeLookup{client: client, repo: repo}), pin)
	if err != nil {
		_ = gitfetch.Remove(dir)
		return gitfetch.Repository{}, err
	}
	return built, nil
}

// discardMergeRepository removes a repository the caller is finished with,
// successfully or not.
func discardMergeRepository(repo gitfetch.Repository) error {
	if repo.Dir == "" {
		return nil
	}
	return gitfetch.Remove(repo.Dir)
}
