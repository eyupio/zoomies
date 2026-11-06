package github

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

// ConflictChange is one file the merged tree differs from the pull request head
// by. Content is the file's bytes; SHA is the commit for a submodule. Deleted
// means the merge removes the path.
type ConflictChange struct {
	Path    string
	Mode    string
	Content []byte
	SHA     string
	Deleted bool
}

// CommitConflictResolution builds the two-parent commit that merges the pinned
// base into the pull request branch, then advances the branch without force.
//
// The head is always the first parent: this is a merge of the base into the
// pull request, not the pull request into its target. Nothing calls the pull
// request merge endpoint. The pull request is read again before the tree is
// built and again before the ref moves, so a concurrent head or base movement
// discards the work rather than publishing a commit for a stale incident.
func (c *appClient) CommitConflictResolution(ctx context.Context, repo string, p RepairPull, baseSHA string, changes []ConflictChange) (string, error) {
	if strings.TrimSpace(baseSHA) == "" {
		return "", fmt.Errorf("a conflict resolution needs the pinned target tip")
	}
	if len(changes) == 0 {
		return "", fmt.Errorf("a conflict resolution has no merged changes to publish")
	}
	if err := c.conflictUnchanged(ctx, repo, p, baseSHA); err != nil {
		return "", err
	}

	base, err := repairRepo(repo)
	if err != nil {
		return "", err
	}
	var parent struct {
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	}
	if err := c.repairRequest(ctx, http.MethodGet, base+"/git/commits/"+p.HeadSHA, nil, &parent); err != nil {
		return "", err
	}
	if parent.Tree.SHA == "" {
		return "", fmt.Errorf("the pull request head has no tree to merge into")
	}
	entries := make([]map[string]any, 0, len(changes))
	for _, change := range changes {
		if change.Path == "" {
			return "", fmt.Errorf("a merged change names no path")
		}
		if change.Deleted {
			// GitHub deletes a path from the base tree with a null sha.
			entries = append(entries, map[string]any{"path": change.Path, "mode": "100644", "type": "blob", "sha": nil})
			continue
		}
		mode := change.Mode
		if mode == "" {
			mode = "100644"
		}
		switch mode {
		case "100644", "100755", "120000":
			entries = append(entries, map[string]any{"path": change.Path, "mode": mode, "type": "blob", "content": string(change.Content)})
		case "160000":
			if change.SHA == "" {
				return "", fmt.Errorf("the submodule %s has no commit id", change.Path)
			}
			entries = append(entries, map[string]any{"path": change.Path, "mode": mode, "type": "commit", "sha": change.SHA})
		default:
			return "", fmt.Errorf("the merged change %s has mode %q, which this commit cannot carry", change.Path, mode)
		}
	}
	var tree struct {
		SHA string `json:"sha"`
	}
	if err := c.repairRequest(ctx, http.MethodPost, base+"/git/trees", map[string]any{
		"base_tree": parent.Tree.SHA,
		"tree":      entries,
	}, &tree); err != nil {
		return "", err
	}
	if tree.SHA == "" {
		return "", fmt.Errorf("GitHub created no tree for the conflict resolution")
	}
	var commit struct {
		SHA string `json:"sha"`
	}
	if err := c.repairRequest(ctx, http.MethodPost, base+"/git/commits", map[string]any{
		"message": "Resolve merge conflicts with Eli",
		"tree":    tree.SHA,
		"parents": []string{p.HeadSHA, baseSHA},
	}, &commit); err != nil {
		return "", err
	}
	if commit.SHA == "" {
		return "", fmt.Errorf("GitHub created no commit for the conflict resolution")
	}
	// The tree exists only on this side so far; a movement now would make the
	// ref update the first write anyone sees, which is what this last read
	// exists to prevent.
	if err := c.conflictUnchanged(ctx, repo, p, baseSHA); err != nil {
		return "", err
	}
	if err := c.repairRequest(ctx, http.MethodPatch, base+"/git/refs/heads/"+p.Branch, map[string]any{
		"sha":   commit.SHA,
		"force": false,
	}, nil); err != nil {
		return "", err
	}
	return commit.SHA, nil
}

// conflictUnchanged reads the pull request again and refuses a head, branch or
// target tip that moved since the caller pinned it.
func (c *appClient) conflictUnchanged(ctx context.Context, repo string, p RepairPull, baseSHA string) error {
	fresh, err := c.RepairPull(ctx, repo, p.Number)
	if err != nil {
		return err
	}
	if fresh.HeadSHA != p.HeadSHA || fresh.Branch != p.Branch || fresh.BaseSHA != baseSHA {
		return ErrSetupConflict
	}
	return nil
}
