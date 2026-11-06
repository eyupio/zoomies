// Package gitmerge computes a bounded three-way merge in a repository the
// controller fetched. It is the trusted half of Eli's conflict repair: it runs
// no repository code, no hooks and no external merge driver, and it returns the
// conflicting text for a model rather than publishing a conflict-marked tree.
package gitmerge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

var (
	// ErrInvalidRequest says a path, commit or limit is not something this
	// worker will read.
	ErrInvalidRequest = errors.New("gitmerge: invalid request")
	// ErrMergeBaseMismatch says the repository's answer differs from the merge
	// base the caller pinned, so the merge would describe a different incident.
	ErrMergeBaseMismatch = errors.New("gitmerge: merge base does not match the pinned ancestor")
	// ErrUnsafeRepository says the repository carries a merge driver this worker
	// will not let Git run.
	ErrUnsafeRepository = errors.New("gitmerge: repository configures an external merge driver")
)

const (
	defaultMaxConflicts = 50
	defaultMaxFileBytes = 256 << 10
	defaultMaxTotal     = 1 << 20
	// A merge commit is built by sending the controller every file the merged
	// tree has that the pull request head does not. That set is bounded for the
	// same reason the conflict text is: a merge that changed the world is one a
	// repair should decline rather than spend a worker's answer on.
	defaultMaxChanges     = 4000
	defaultMaxChangeBytes = 4 << 20
)

// Request names the repository and the three commits of one merge. Ancestor is
// the merge base the caller pinned; the worker refuses if the repository
// computes a different one.
type Request struct {
	Dir          string
	Ancestor     string
	Base         string
	Head         string
	MaxConflicts int
	MaxFileBytes int
	MaxTotal     int
}

// Conflict is one supported text conflict, with the three versions the model
// may read. A side absent from the merge is empty.
type Conflict struct {
	Path     string `json:"path"`
	Mode     string `json:"mode,omitempty"`
	Ancestor string `json:"ancestor"`
	Base     string `json:"base"`
	Head     string `json:"head"`
}

// UnsupportedConflict is a conflict this worker will not hand to a model.
type UnsupportedConflict struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// Change is one file the merged tree has that the pull request head does not:
// added, modified, deleted or changed in mode. It is what lets the controller
// build the merge commit without the worker's repository, where the merged tree
// object exists but GitHub has never seen it.
//
// Content is the file's bytes for a blob; SHA is the commit for a submodule.
// Deleted means the merged tree has no entry at Path.
type Change struct {
	Path    string `json:"path"`
	Mode    string `json:"mode,omitempty"`
	Content []byte `json:"content,omitempty"`
	SHA     string `json:"sha,omitempty"`
	Deleted bool   `json:"deleted,omitempty"`
}

// Result is one merge attempt. Tree may contain conflict markers, so a caller
// must not publish it while Conflicts is non-empty.
type Result struct {
	Tree        string                `json:"tree"`
	Conflicts   []Conflict            `json:"conflicts,omitempty"`
	Unsupported []UnsupportedConflict `json:"unsupported,omitempty"`
	Truncated   bool                  `json:"truncated,omitempty"`
	// Changes is what turns the merged tree into something GitHub can commit:
	// the files it differs from the pull request head by.
	Changes []Change `json:"changes,omitempty"`
}

// Merge runs git merge-tree against the pinned ancestor. A clean merge has no
// conflicts and its tree is publishable. A conflicted merge returns the text of
// every supported conflict and names the ones it refused.
func Merge(ctx context.Context, in Request) (Result, error) {
	if err := validate(in); err != nil {
		return Result{}, err
	}
	if in.MaxConflicts <= 0 {
		in.MaxConflicts = defaultMaxConflicts
	}
	if in.MaxFileBytes <= 0 {
		in.MaxFileBytes = defaultMaxFileBytes
	}
	if in.MaxTotal <= 0 {
		in.MaxTotal = defaultMaxTotal
	}
	if drivers, err := run(ctx, in.Dir, "config", "--local", "--get-regexp", `^merge\..*\.driver$`); err == nil && len(bytes.TrimSpace(drivers)) > 0 {
		return Result{}, ErrUnsafeRepository
	}
	computed, err := run(ctx, in.Dir, "merge-base", in.Base, in.Head)
	if err != nil {
		return Result{}, fmt.Errorf("gitmerge: read merge base: %w", err)
	}
	if strings.TrimSpace(string(computed)) != in.Ancestor {
		return Result{}, ErrMergeBaseMismatch
	}
	// Two commits, not three: the ancestor was just checked against what the
	// repository computes, so merge-tree re-deriving it is the same merge. The
	// flag that would say it explicitly (--merge-base) arrived after the Git in
	// Debian 12, which is the floor a controller host can meet, and a worker
	// that needs a newer Git than the host has is a conflict nobody can repair.
	out, err := run(ctx, in.Dir, "merge-tree", "--write-tree", "--name-only", "--no-messages", "-z", in.Base, in.Head)
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 {
			return Result{}, fmt.Errorf("gitmerge: merge tree: %w", err)
		}
	}
	parts := bytes.Split(out, []byte{0})
	if len(parts) == 0 || len(parts[0]) == 0 {
		return Result{}, errors.New("gitmerge: merge tree returned no tree")
	}
	result := Result{Tree: string(parts[0])}
	seen := map[string]bool{}
	total := 0
	for _, raw := range parts[1:] {
		path := string(raw)
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		if len(result.Conflicts)+len(result.Unsupported) >= in.MaxConflicts {
			result.Truncated = true
			break
		}
		conflict, unsupported := readConflict(ctx, in, path, &total)
		if unsupported != "" {
			result.Unsupported = append(result.Unsupported, UnsupportedConflict{Path: path, Reason: unsupported})
			continue
		}
		if total > in.MaxTotal {
			result.Unsupported = append(result.Unsupported, UnsupportedConflict{Path: path, Reason: "conflict text exceeds the merge worker's budget"})
			continue
		}
		result.Conflicts = append(result.Conflicts, conflict)
	}
	changes, err := readChanges(ctx, in, result.Tree)
	if err != nil {
		return Result{}, err
	}
	result.Changes = changes
	return result, nil
}

// readChanges lists what the merged tree changes relative to the pull request
// head. The head is the first parent of the commit the controller will build,
// so this is exactly the tree difference that commit must carry.
func readChanges(ctx context.Context, in Request, tree string) ([]Change, error) {
	out, err := run(ctx, in.Dir, "diff-tree", "--no-commit-id", "--name-status", "-r", "-z", "--no-renames", in.Head, tree)
	if err != nil {
		return nil, fmt.Errorf("gitmerge: listing the merged tree: %w", err)
	}
	parts := bytes.Split(out, []byte{0})
	changes := make([]Change, 0, len(parts)/2)
	total := 0
	for i := 0; i < len(parts); {
		status := string(parts[i])
		i++
		if status == "" {
			continue
		}
		if i >= len(parts) || parts[i] == nil {
			return nil, errors.New("gitmerge: the merged tree's change list is malformed")
		}
		path := string(parts[i])
		i++
		if path == "" {
			return nil, errors.New("gitmerge: the merged tree's change list names an empty path")
		}
		if len(changes) >= defaultMaxChanges {
			return nil, fmt.Errorf("%w: the merge changes more than %d files", ErrTooLarge, defaultMaxChanges)
		}
		switch status {
		case "D":
			changes = append(changes, Change{Path: path, Deleted: true})
			continue
		case "A", "M", "T":
		default:
			return nil, fmt.Errorf("gitmerge: the merged tree change %q has an unsupported status %q", path, status)
		}
		change, err := readChange(ctx, in, tree, path)
		if err != nil {
			return nil, err
		}
		total += len(change.Content)
		if total > defaultMaxChangeBytes {
			return nil, fmt.Errorf("%w: the merged tree changes more than %d bytes", ErrTooLarge, defaultMaxChangeBytes)
		}
		changes = append(changes, change)
	}
	return changes, nil
}

// readChange reads one entry of the merged tree.
func readChange(ctx context.Context, in Request, tree, path string) (Change, error) {
	out, err := run(ctx, in.Dir, "ls-tree", "-z", "--full-tree", tree, "--", path)
	if err != nil {
		return Change{}, fmt.Errorf("gitmerge: reading %s from the merged tree: %w", path, err)
	}
	entry := strings.TrimSuffix(string(out), "\x00")
	meta, _, ok := strings.Cut(entry, "\t")
	if !ok {
		return Change{}, fmt.Errorf("gitmerge: the merged tree entry for %s is malformed", path)
	}
	fields := strings.Fields(meta)
	if len(fields) != 3 {
		return Change{}, fmt.Errorf("gitmerge: the merged tree entry for %s is malformed", path)
	}
	mode, kind, object := fields[0], fields[1], fields[2]
	change := Change{Path: path, Mode: mode}
	switch {
	case kind == "commit" || mode == "160000":
		change.SHA = object
		return change, nil
	case kind == "blob":
		content, err := run(ctx, in.Dir, "cat-file", "blob", object)
		if err != nil {
			return Change{}, fmt.Errorf("gitmerge: reading the merged content of %s: %w", path, err)
		}
		change.Content = content
		return change, nil
	default:
		return Change{}, fmt.Errorf("gitmerge: the merged tree entry for %s is a %s, which is not publishable this way", path, kind)
	}
}

func validate(in Request) error {
	if strings.TrimSpace(in.Dir) == "" {
		return fmt.Errorf("%w: no repository directory", ErrInvalidRequest)
	}
	info, err := os.Stat(in.Dir)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("%w: repository directory is not readable", ErrInvalidRequest)
	}
	for name, sha := range map[string]string{"ancestor": in.Ancestor, "base": in.Base, "head": in.Head} {
		if !objectID(sha) {
			return fmt.Errorf("%w: %s is not a commit id", ErrInvalidRequest, name)
		}
	}
	if in.MaxConflicts < 0 || in.MaxFileBytes < 0 || in.MaxTotal < 0 {
		return fmt.Errorf("%w: limits cannot be negative", ErrInvalidRequest)
	}
	return nil
}

func objectID(s string) bool {
	if len(s) < 40 || len(s) > 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

func readConflict(ctx context.Context, in Request, path string, total *int) (Conflict, string) {
	ancestor, reason := side(ctx, in, in.Ancestor, path, total)
	if reason != "" {
		return Conflict{}, reason
	}
	base, reason := side(ctx, in, in.Base, path, total)
	if reason != "" {
		return Conflict{}, reason
	}
	head, reason := side(ctx, in, in.Head, path, total)
	if reason != "" {
		return Conflict{}, reason
	}
	if base.Absent || head.Absent {
		return Conflict{}, "delete or modify on one side"
	}
	if ancestor.Absent && (base.Mode != head.Mode) {
		return Conflict{}, "added on both sides with different file modes"
	}
	if !ancestor.Absent && base.Mode != ancestor.Mode || !ancestor.Absent && head.Mode != ancestor.Mode || base.Mode != head.Mode {
		return Conflict{}, "file mode differs across the merge"
	}
	if base.Mode == "120000" || base.Mode == "160000" || head.Mode == "120000" || head.Mode == "160000" || ancestor.Mode == "120000" || ancestor.Mode == "160000" {
		return Conflict{}, "symlinks and submodules are not resolved by a model"
	}
	if base.Binary || head.Binary || ancestor.Binary {
		return Conflict{}, "binary content is not resolved by a model"
	}
	return Conflict{Path: path, Mode: base.Mode, Ancestor: ancestor.Text, Base: base.Text, Head: head.Text}, ""
}

type blobSide struct {
	Mode   string
	Text   string
	Absent bool
	Binary bool
}

func side(ctx context.Context, in Request, commit, path string, total *int) (blobSide, string) {
	out, err := run(ctx, in.Dir, "ls-tree", "-z", "--full-tree", commit, "--", path)
	if err != nil {
		return blobSide{}, fmt.Sprintf("read tree entry: %v", err)
	}
	entry := strings.TrimSuffix(string(out), "\x00")
	if entry == "" {
		return blobSide{Absent: true}, ""
	}
	meta, _, ok := strings.Cut(entry, "\t")
	if !ok {
		return blobSide{}, "tree entry is malformed"
	}
	fields := strings.Fields(meta)
	if len(fields) != 3 {
		return blobSide{}, "tree entry is malformed"
	}
	mode, kind, object := fields[0], fields[1], fields[2]
	if kind != "blob" || mode == "120000" || mode == "160000" {
		return blobSide{Mode: mode}, ""
	}
	sizeOut, err := run(ctx, in.Dir, "cat-file", "-s", object)
	if err != nil {
		return blobSide{}, fmt.Sprintf("read blob size: %v", err)
	}
	size := 0
	if _, err := fmt.Sscan(strings.TrimSpace(string(sizeOut)), &size); err != nil {
		return blobSide{}, "blob size is malformed"
	}
	if size > in.MaxFileBytes || *total+size > in.MaxTotal {
		return blobSide{Mode: mode}, "conflict text exceeds the merge worker's budget"
	}
	content, err := run(ctx, in.Dir, "cat-file", "blob", object)
	if err != nil {
		return blobSide{}, fmt.Sprintf("read blob: %v", err)
	}
	if bytes.IndexByte(content, 0) >= 0 {
		return blobSide{Mode: mode, Binary: true}, ""
	}
	*total += len(content)
	return blobSide{Mode: mode, Text: string(content)}, ""
}

func run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	gitArgs := append([]string{"-c", "safe.directory=" + dir, "-c", "core.hooksPath=/dev/null", "-c", "core.attributesFile=/dev/null", "--no-pager"}, args...)
	cmd := exec.CommandContext(ctx, "git", gitArgs...)
	cmd.Dir = dir
	cmd.Env = append(sanitisedEnv(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_ATTR_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
	return cmd.Output()
}

func sanitisedEnv() []string {
	out := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		switch {
		case strings.HasPrefix(kv, "GIT_"),
			strings.HasPrefix(kv, "SSH_ASKPASS="),
			strings.HasPrefix(kv, "GIT_ASKPASS="):
			continue
		}
		out = append(out, kv)
	}
	return out
}
