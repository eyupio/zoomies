// Package gitbare writes a bare Git repository from the file lists a provider's
// per-commit archive gave the controller.
//
// Decision 0017 says why the controller builds the worker's repository instead of
// fetching one with Git: the controller has no Git, runs no repository code, and
// holds the only credential in the system, so the repository has to arrive as
// content the controller hashes rather than as a clone it requests. Blobs and
// trees are content-addressed, so every id written here is the id Git itself
// would have written, and the root tree each commit claims is checked by hashing
// it rather than trusted.
package gitbare

import (
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// File is one file of one commit, as the provider's archive listed it. Mode is
// Git's own: 100644 for a file, 100755 for an executable, 120000 for a symlink
// whose content is the target path.
type File struct {
	Path    string
	Mode    string
	Content []byte
}

// Commit is one commit to write into the repository.
type Commit struct {
	// Parents are the ids of the objects this commit follows, in order. They are
	// the ids Write gave them, not the provider's: the archive carries no commit
	// objects, so the graph is the controller's.
	Parents []string
	// Tree is the root tree id the provider reported for this commit. Write
	// refuses a reconstruction whose tree does not hash to it, which is what
	// makes a silent disagreement with the provider impossible.
	Tree string
	// When dates the commit. The archive carries no dates, so the controller
	// supplies the instant it is reconstructing at.
	When time.Time
	// Message is the commit message the controller writes.
	Message string
	// Files are every file in this commit's tree.
	Files []File
}

// TreeID writes one tree's objects into dir and returns the id those files hash
// to, without a commit.
//
// It is what a caller checks a provider's reported tree against when there is no
// commit to write, and what a test uses to make a fixture whose tree and archive
// agree by construction rather than by transcription.
func TreeID(dir string, files []File) (string, error) {
	if err := initRepository(dir); err != nil {
		return "", err
	}
	return writeTree(dir, filesByDirectory(files))
}

// Write creates the bare repository at dir, writes every commit's objects and
// returns the commit ids in the order the commits were given.
//
// The repository holds objects and nothing else worth having: no hooks, no
// configuration beyond the format version, no remotes and no credentials. That is
// the shape the merge worker's request expects.
func Write(dir string, commits []Commit) ([]string, error) {
	if err := initRepository(dir); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(commits))
	for i, commit := range commits {
		root, err := writeTree(dir, filesByDirectory(commit.Files))
		if err != nil {
			return nil, fmt.Errorf("commit %d: %w", i, err)
		}
		if commit.Tree != "" && root != commit.Tree {
			// The one check that matters: a reconstruction that disagrees with
			// the provider would merge a tree nobody asked about, and the ids
			// being content-addressed is what makes this comparison meaningful.
			return nil, fmt.Errorf("commit %d: the reconstructed tree %s is not the tree the provider reported, %s", i, root, commit.Tree)
		}
		id, err := writeCommit(dir, commit, root)
		if err != nil {
			return nil, fmt.Errorf("commit %d: %w", i, err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// initRepository writes the four files that make a directory a bare repository
// Git will read, and nothing else.
func initRepository(dir string) error {
	if err := os.MkdirAll(filepath.Join(dir, "objects"), 0o755); err != nil {
		return err
	}
	for _, sub := range []string{"refs/heads", "refs/tags"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return err
		}
	}
	config := "[core]\n\trepositoryformatversion = 0\n\tfilemode = true\n\tbare = true\n"
	if err := os.WriteFile(filepath.Join(dir, "config"), []byte(config), 0o644); err != nil {
		return err
	}
	// A HEAD that names a branch nothing has written: the worker is given commit
	// ids, never a branch, so what HEAD points at must not decide anything. It
	// exists because Git refuses a repository without one.
	return os.WriteFile(filepath.Join(dir, "HEAD"), []byte("ref: refs/heads/none\n"), 0o644)
}

// directory is one directory of one commit's tree, keyed by name.
type directory struct {
	files map[string]File
	dirs  map[string]*directory
}

// filesByDirectory groups the archive's flat paths into the tree they describe.
//
// A path that is absolute, empty or climbs with a ".." segment is dropped rather
// than repaired: an archive's paths are untrusted input, and a path that escapes
// the repository would be written outside it. Dropping one cannot go unnoticed,
// because the tree it would have made no longer matches the one Write checks.
func filesByDirectory(files []File) *directory {
	root := &directory{}
	for _, file := range files {
		clean, ok := insideRepository(file.Path)
		if !ok {
			continue
		}
		parts := strings.Split(clean, "/")
		node := root
		for _, part := range parts[:len(parts)-1] {
			if node.dirs == nil {
				node.dirs = map[string]*directory{}
			}
			next, existing := node.dirs[part]
			if !existing {
				next = &directory{}
				node.dirs[part] = next
			}
			node = next
		}
		if node.files == nil {
			node.files = map[string]File{}
		}
		node.files[parts[len(parts)-1]] = file
	}
	return root
}

// insideRepository reports whether a path names something inside the repository,
// and what that path is once its redundant separators are gone.
func insideRepository(name string) (string, bool) {
	slashed := strings.ReplaceAll(name, "\\", "/")
	if slashed == "" || strings.HasPrefix(slashed, "/") {
		return "", false
	}
	for _, segment := range strings.Split(slashed, "/") {
		if segment == ".." {
			return "", false
		}
	}
	clean := path.Clean(slashed)
	if clean == "." || clean == "" {
		return "", false
	}
	return clean, true
}

// writeTree writes one directory's tree, deepest first, and returns its id.
func writeTree(repo string, node *directory) (string, error) {
	var entries []treeEntry
	for name, child := range node.dirs {
		id, err := writeTree(repo, child)
		if err != nil {
			return "", err
		}
		entries = append(entries, treeEntry{mode: modeTree, name: name, id: id, directory: true})
	}
	for name, file := range node.files {
		mode := file.Mode
		if mode == "" {
			mode = "100644"
		}
		id, err := writeObject(repo, "blob", file.Content)
		if err != nil {
			return "", err
		}
		entries = append(entries, treeEntry{mode: mode, name: name, id: id})
	}
	// Git sorts a tree by name, and compares a directory as though its name
	// ended in "/". Sorting any other way writes a tree whose bytes differ from
	// the ones Git writes, so its id differs and the check in Write would refuse
	// every commit.
	sort.Slice(entries, func(i, j int) bool {
		left, right := entries[i], entries[j]
		if left.directory {
			left.name += "/"
		}
		if right.directory {
			right.name += "/"
		}
		return left.name < right.name
	})
	var body bytes.Buffer
	for _, entry := range entries {
		fmt.Fprintf(&body, "%s %s\x00", entry.mode, entry.name)
		raw, err := hex.DecodeString(entry.id)
		if err != nil {
			return "", err
		}
		body.Write(raw)
	}
	return writeObject(repo, "tree", body.Bytes())
}

const modeTree = "40000"

type treeEntry struct {
	mode      string
	name      string
	id        string
	directory bool
}

// writeCommit writes the commit object. Its id is the controller's rather than
// the provider's, because the archive carries no author, committer or message;
// the identity that matters is the tree, and Write has already checked it.
func writeCommit(repo string, commit Commit, tree string) (string, error) {
	when := commit.When
	if when.IsZero() {
		when = time.Unix(0, 0).UTC()
	}
	stamp := fmt.Sprintf("%d +0000", when.UTC().Unix())
	var body bytes.Buffer
	fmt.Fprintf(&body, "tree %s\n", tree)
	for _, parent := range commit.Parents {
		fmt.Fprintf(&body, "parent %s\n", parent)
	}
	fmt.Fprintf(&body, "author Zoomies <zoomies@zoomies.invalid> %s\n", stamp)
	fmt.Fprintf(&body, "committer Zoomies <zoomies@zoomies.invalid> %s\n\n", stamp)
	body.WriteString(strings.TrimRight(commit.Message, "\n"))
	body.WriteString("\n")
	return writeObject(repo, "commit", body.Bytes())
}

// A zlib writer carries a compressor of about a megabyte, and a repository is
// written one object at a time, so a writer made for each object was nearly all
// of what writing cost: about 1 MB of garbage per file, which on a tree of
// thousands is gigabytes for the controller's collector to chase. The writer is
// reset onto each object instead.
var objectWriters = sync.Pool{New: func() any { return zlib.NewWriter(io.Discard) }}

// maxPooledBuffer is the largest compressed object whose buffer is kept. One big
// file must not leave a buffer that size in the pool for the life of the process.
const maxPooledBuffer = 1 << 20

var objectBuffers = sync.Pool{New: func() any { return new(bytes.Buffer) }}

// writeObject writes one loose object and returns its id: the SHA-1 of the header
// and the content, which is exactly how Git names it.
func writeObject(repo, kind string, content []byte) (string, error) {
	header := fmt.Sprintf("%s %d\x00", kind, len(content))
	hash := sha1.New()
	hash.Write([]byte(header))
	hash.Write(content)
	id := hex.EncodeToString(hash.Sum(nil))

	target := filepath.Join(repo, "objects", id[:2], id[2:])
	if _, err := os.Stat(target); err == nil {
		return id, nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", err
	}
	compressed := objectBuffers.Get().(*bytes.Buffer)
	compressed.Reset()
	defer func() {
		if compressed.Cap() <= maxPooledBuffer {
			objectBuffers.Put(compressed)
		}
	}()
	writer := objectWriters.Get().(*zlib.Writer)
	defer objectWriters.Put(writer)
	writer.Reset(compressed)
	if _, err := writer.Write([]byte(header)); err != nil {
		return "", err
	}
	if _, err := writer.Write(content); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	// Loose objects are read-only, as Git writes them: a repository whose objects
	// can be rewritten is one whose objects can disagree with their names.
	return id, os.WriteFile(target, compressed.Bytes(), 0o444)
}
