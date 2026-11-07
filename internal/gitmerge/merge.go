// Package gitmerge computes a bounded three-way merge in a repository the
// controller fetched. It is the trusted half of Eli's conflict repair: it runs
// no repository code, no hooks and no external merge driver, and it returns the
// conflicting text for a model rather than publishing a conflict-marked tree.
package gitmerge

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
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
//
// One diff-tree names every path with its new mode and object id, and one
// cat-file reads all the blobs, so the cost is two processes however many files
// the merge brought in rather than two for each of them.
func readChanges(ctx context.Context, in Request, tree string) ([]Change, error) {
	out, err := run(ctx, in.Dir, "diff-tree", "--no-commit-id", "--raw", "-z", "--no-abbrev", "-r", "--no-renames", in.Head, tree)
	if err != nil {
		return nil, fmt.Errorf("gitmerge: listing the merged tree: %w", err)
	}
	parts := bytes.Split(out, []byte{0})
	changes := make([]Change, 0, len(parts)/2)
	// blobs[i] is the object whose bytes become changes[blobAt[i]].Content.
	var blobs []string
	var blobAt []int
	for i := 0; i < len(parts); {
		record := string(parts[i])
		i++
		if record == "" {
			continue
		}
		if !strings.HasPrefix(record, ":") || i >= len(parts) {
			return nil, errors.New("gitmerge: the merged tree's change list is malformed")
		}
		path := string(parts[i])
		i++
		if path == "" {
			return nil, errors.New("gitmerge: the merged tree's change list names an empty path")
		}
		fields := strings.Fields(record[1:])
		if len(fields) != 5 {
			return nil, errors.New("gitmerge: the merged tree's change list is malformed")
		}
		mode, object, status := fields[1], fields[3], fields[4]
		if len(changes) >= defaultMaxChanges {
			return nil, fmt.Errorf("%w: the merge changes more than %d files", ErrTooLarge, defaultMaxChanges)
		}
		switch status {
		case "D":
			changes = append(changes, Change{Path: path, Deleted: true})
		case "A", "M", "T":
			change := Change{Path: path, Mode: mode}
			switch mode {
			case "160000":
				change.SHA = object
			case "100644", "100755", "120000":
				blobs = append(blobs, object)
				blobAt = append(blobAt, len(changes))
			default:
				return nil, fmt.Errorf("gitmerge: the merged tree entry for %s has mode %s, which is not publishable this way", path, mode)
			}
			changes = append(changes, change)
		default:
			return nil, fmt.Errorf("gitmerge: the merged tree change %q has an unsupported status %q", path, status)
		}
	}
	contents, err := readBlobs(ctx, in.Dir, blobs, defaultMaxChangeBytes)
	if err != nil {
		return nil, err
	}
	for i, content := range contents {
		changes[blobAt[i]].Content = content
	}
	return changes, nil
}

// readBlobs reads the named blobs with one cat-file, in order, and refuses to go
// past limit bytes in all. The size comes before the bytes, so a blob over the
// budget is declined without being read.
func readBlobs(ctx context.Context, dir string, objects []string, limit int) ([][]byte, error) {
	if len(objects) == 0 {
		return nil, nil
	}
	cmd := gitCommand(ctx, dir, "cat-file", "--batch")
	cmd.Stdin = strings.NewReader(strings.Join(objects, "\n") + "\n")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("gitmerge: reading the merged content: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("gitmerge: reading the merged content: %w", err)
	}
	abandon := func(err error) ([][]byte, error) {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, err
	}
	reader := bufio.NewReader(stdout)
	contents := make([][]byte, 0, len(objects))
	total := 0
	for _, object := range objects {
		header, err := reader.ReadString('\n')
		if err != nil {
			return abandon(fmt.Errorf("gitmerge: reading the merged content of %s: %w%s", object, err, stderrNote(stderr.Bytes())))
		}
		fields := strings.Fields(header)
		if len(fields) != 3 || fields[1] != "blob" {
			return abandon(fmt.Errorf("gitmerge: the merged tree object %s is not a blob git can read: %q", object, strings.TrimSpace(header)))
		}
		size, err := strconv.Atoi(fields[2])
		if err != nil || size < 0 {
			return abandon(fmt.Errorf("gitmerge: the size of the merged blob %s is malformed", object))
		}
		if size > limit-total {
			return abandon(fmt.Errorf("%w: the merged tree changes more than %d bytes", ErrTooLarge, limit))
		}
		content := make([]byte, size)
		if _, err := io.ReadFull(reader, content); err != nil {
			return abandon(fmt.Errorf("gitmerge: reading the merged content of %s: %w", object, err))
		}
		// Each object is followed by a newline that is not part of it.
		if _, err := reader.ReadByte(); err != nil {
			return abandon(fmt.Errorf("gitmerge: reading the merged content of %s: %w", object, err))
		}
		total += size
		contents = append(contents, content)
	}
	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("gitmerge: reading the merged content: %w%s", err, stderrNote(stderr.Bytes()))
	}
	return contents, nil
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

// maxGitStderr bounds the part of Git's own complaint that is kept in an error.
const maxGitStderr = 512

// gitCommand is Git run the way this worker allows it: no hooks, no attributes
// file, no system or user configuration and none of the caller's GIT_ variables.
func gitCommand(ctx context.Context, dir string, args ...string) *exec.Cmd {
	gitArgs := append([]string{"-c", "safe.directory=" + dir, "-c", "core.hooksPath=/dev/null", "-c", "core.attributesFile=/dev/null", "--no-pager"}, args...)
	cmd := exec.CommandContext(ctx, "git", gitArgs...)
	cmd.Dir = dir
	cmd.Env = append(sanitisedEnv(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_ATTR_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
	return cmd
}

// run returns Git's output. A failure keeps what Git said on standard error, since
// "exit status 128" is all an operator would otherwise have to go on, and still
// wraps the *exec.ExitError so a caller can read the exit code.
func run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	out, err := gitCommand(ctx, dir, args...).Output()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		if note := stderrNote(exit.Stderr); note != "" {
			return out, fmt.Errorf("%w%s", err, note)
		}
	}
	return out, err
}

// stderrNote renders Git's complaint as a suffix for an error, or nothing.
func stderrNote(stderr []byte) string {
	msg := strings.TrimSpace(string(stderr))
	if msg == "" {
		return ""
	}
	if len(msg) > maxGitStderr {
		msg = msg[:maxGitStderr] + "..."
	}
	return ": " + msg
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
