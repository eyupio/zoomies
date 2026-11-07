// Package gitfetch turns three pinned commits into the bare repository a Git
// worker merges.
//
// Decision 0017 says the controller builds that repository rather than cloning
// it, and decision 0018 says an agent host runs the worker. This package is the
// controller's half: it asks a Source for one commit at a time, writes what it is
// given with `internal/gitbare`, and refuses anything that does not add up. It
// holds no credential and makes no network call of its own, which is what makes
// it testable against a real repository and against the worker without either.
package gitfetch

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/eyupio/zoomies/internal/gitbare"
)

var (
	// ErrUnpinned says the incident's three commits are not the three ids a
	// provider would have issued.
	ErrUnpinned = errors.New("gitfetch: the pinned commits are not commit ids")
	// ErrWrongCommit says the source answered for a commit nobody asked about,
	// which would silently merge a different incident.
	ErrWrongCommit = errors.New("gitfetch: the source answered for another commit")
)

// Pin is the incident: the merge base GitHub reported, the target tip, and the
// pull request's head, exactly as the watch pinned them.
type Pin struct {
	Ancestor string
	Base     string
	Head     string
}

// Commit is one commit as a provider describes it: the tree id it reports, the
// commits it follows, and the file the archive gave for it.
type Commit struct {
	SHA     string
	Tree    string
	Parents []string
	Files   []gitbare.File
}

// Lookup is one repository as the controller's GitHub client sees it: the tree a
// commit points at with the commits it follows, and the files of that tree.
//
// It is deliberately two calls rather than one, because they come from two
// places: the commit is an API read and the files are the archive. The whole
// point of the pair is that they can disagree, which is what Build checks.
type Lookup interface {
	Commit(ctx context.Context, commit string) (tree string, parents []string, err error)
	Files(ctx context.Context, commit string) ([]gitbare.File, error)
}

// NewSource turns a Lookup into the Source Build asks for one commit at a time.
func NewSource(l Lookup) Source { return lookupSource{lookup: l} }

type lookupSource struct{ lookup Lookup }

func (s lookupSource) Commit(ctx context.Context, sha string) (Commit, error) {
	tree, parents, err := s.lookup.Commit(ctx, sha)
	if err != nil {
		return Commit{}, err
	}
	files, err := s.lookup.Files(ctx, sha)
	if err != nil {
		return Commit{}, err
	}
	return Commit{SHA: sha, Tree: tree, Parents: parents, Files: files}, nil
}

// Source hands back one commit at a time. The controller's implementation asks
// GitHub for the commit and its archive; a test passes a real repository.
type Source interface {
	Commit(ctx context.Context, sha string) (Commit, error)
}

// Repository is what Build wrote: the directory an agent is asked to unpack and
// mount, and the three ids the worker's request names.
//
// The ids are the writer's rather than the provider's, because an archive carries
// no commit objects. The trees are the provider's, which is the identity that
// matters: `gitbare.Write` refuses a commit whose files do not hash to the tree
// the provider reported.
type Repository struct {
	Dir      string
	Ancestor string
	Base     string
	Head     string
}

// Build writes the three commits into dir and returns the repository.
//
// The ancestor is written first and named as the parent of both sides, which is
// the graph a three-way merge needs: the provider's real parents may reach back
// much further, and a merge that walked them would answer a different question
// from the one the watch pinned.
func Build(ctx context.Context, dir string, src Source, pin Pin) (Repository, error) {
	if !commitID(pin.Ancestor) || !commitID(pin.Base) || !commitID(pin.Head) {
		return Repository{}, ErrUnpinned
	}

	ancestor, err := fetch(ctx, src, pin.Ancestor)
	if err != nil {
		return Repository{}, fmt.Errorf("the merge base: %w", err)
	}
	ids, err := gitbare.Write(dir, []gitbare.Commit{{
		Tree:    ancestor.Tree,
		Message: "merge base " + short(pin.Ancestor),
		Files:   ancestor.Files,
	}})
	if err != nil {
		return Repository{}, fmt.Errorf("writing the merge base: %w", err)
	}
	ancestorID := ids[0]
	ancestor.Files = nil

	// Each side is written before the next is fetched, so only one commit's files
	// are in memory at a time. The ancestor's were released the same way.
	base, err := fetch(ctx, src, pin.Base)
	if err != nil {
		return Repository{}, fmt.Errorf("the target tip: %w", err)
	}
	baseIDs, err := gitbare.Write(dir, []gitbare.Commit{{
		Tree:    base.Tree,
		Parents: []string{ancestorID},
		Message: "target tip " + short(pin.Base),
		Files:   base.Files,
	}})
	if err != nil {
		return Repository{}, fmt.Errorf("writing the target tip: %w", err)
	}
	base.Files = nil
	head, err := fetch(ctx, src, pin.Head)
	if err != nil {
		return Repository{}, fmt.Errorf("the pull request head: %w", err)
	}
	headIDs, err := gitbare.Write(dir, []gitbare.Commit{{
		Tree:    head.Tree,
		Parents: []string{ancestorID},
		Message: "head " + short(pin.Head),
		Files:   head.Files,
	}})
	if err != nil {
		return Repository{}, fmt.Errorf("writing the pull request head: %w", err)
	}
	ids = []string{baseIDs[0], headIDs[0]}
	return Repository{Dir: dir, Ancestor: ancestorID, Base: ids[0], Head: ids[1]}, nil
}

// fetch asks the source for one commit and refuses an answer about another.
func fetch(ctx context.Context, src Source, sha string) (Commit, error) {
	commit, err := src.Commit(ctx, sha)
	if err != nil {
		return Commit{}, err
	}
	if !strings.EqualFold(commit.SHA, sha) {
		return Commit{}, fmt.Errorf("%w: asked for %s and was given %q", ErrWrongCommit, sha, commit.SHA)
	}
	return commit, nil
}

// Remove deletes a built repository.
//
// Loose objects are read-only, as Git writes them, and a read-only file cannot be
// deleted on Windows; clearing the bit first is what makes the same call work on
// every host the controller supports.
func Remove(dir string) error {
	_ = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			return os.Chmod(path, 0o755)
		}
		return os.Chmod(path, 0o644)
	})
	return os.RemoveAll(dir)
}

// commitID reports whether s is a SHA-1 or SHA-256 commit id, which is the one
// shape the worker's request accepts.
func commitID(s string) bool {
	if len(s) != 40 && len(s) != 64 {
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

func short(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}
