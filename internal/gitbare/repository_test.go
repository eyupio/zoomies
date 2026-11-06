package gitbare

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/gitmerge"
)

// git runs one Git command in a working repository and returns its output.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// bareGit runs one Git command against a bare repository this package wrote.
func bareGit(t *testing.T, repo string, args ...string) string {
	t.Helper()
	return git(t, repo, append([]string{"--git-dir=" + repo}, args...)...)
}

// workingRepository builds a repository with the shapes a real one has: nested
// directories, an executable, a symlink, and a file whose name sorts beside a
// directory of the same name.
func workingRepository(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	git(t, dir, "config", "user.email", "test@example.test")
	git(t, dir, "config", "user.name", "Test")
	write := func(name, content string, mode os.FileMode) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
	}
	write("README.md", "hello\n", 0o644)
	write("bin/run.sh", "#!/bin/sh\necho hi\n", 0o755)
	write("src/app/main.go", "package main\n", 0o644)
	write("src/app/util.go", "package main\n\nfunc util() {}\n", 0o644)
	write("src/z.go", "package src\n", 0o644)
	if err := os.Symlink("README.md", filepath.Join(dir, "link.md")); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "one")
	return dir, git(t, dir, "rev-parse", "HEAD")
}

// blob returns a blob's bytes exactly, which git() cannot: it trims, and a blob
// whose last byte is a newline is not the same blob without one.
func blob(t *testing.T, dir, id string) []byte {
	t.Helper()
	cmd := exec.Command("git", "-c", "core.autocrlf=false", "cat-file", "blob", id)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git cat-file blob %s: %v", id, err)
	}
	return out
}

// archiveOf reads a commit the way a provider's archive presents it: every path,
// its mode and its content. It is the input Write is meant to be given.
func archiveOf(t *testing.T, dir, commit string) []File {
	t.Helper()
	listing := git(t, dir, "ls-tree", "-r", "-z", "--full-tree", commit)
	if listing == "" {
		return nil
	}
	var files []File
	for _, entry := range strings.Split(listing, "\x00") {
		if entry == "" {
			continue
		}
		meta, name, ok := strings.Cut(entry, "\t")
		if !ok {
			t.Fatalf("ls-tree entry %q is malformed", entry)
		}
		fields := strings.Fields(meta)
		if len(fields) != 3 {
			t.Fatalf("ls-tree metadata %q is malformed", meta)
		}
		files = append(files, File{Path: name, Mode: fields[0], Content: blob(t, dir, fields[2])})
	}
	return files
}

// treeID reads the root tree id of a commit object this package wrote, so a test
// can claim the tree it just built without a Git checkout.
func treeID(t *testing.T, repo, commit string) string {
	t.Helper()
	out := bareGit(t, repo, "cat-file", "commit", commit)
	tree, ok := strings.CutPrefix(out, "tree ")
	if !ok {
		t.Fatalf("commit %s has no tree line:\n%s", commit, out)
	}
	tree, _, _ = strings.Cut(tree, "\n")
	return strings.TrimSpace(tree)
}

// The whole point of the package: hashing a real commit's files must produce the
// very tree id Git produced, or the repository the worker merges is not the
// repository the provider showed the operator.
func TestWriteReproducesTheTreeGitWrote(t *testing.T) {
	dir, commit := workingRepository(t)
	tree := git(t, dir, "rev-parse", commit+"^{tree}")

	bare := t.TempDir()
	ids, err := Write(bare, []Commit{{
		Tree:    tree,
		When:    time.Unix(1700000000, 0),
		Message: "one",
		Files:   archiveOf(t, dir, commit),
	}})
	if err != nil {
		t.Fatalf("writing the repository: %v", err)
	}
	if len(ids) != 1 || len(ids[0]) != 40 {
		t.Fatalf("ids %v", ids)
	}
	if got := treeID(t, bare, ids[0]); got != tree {
		t.Fatalf("the commit points at tree %s, want %s", got, tree)
	}

	want := git(t, dir, "ls-tree", "-r", "--full-tree", commit)
	got := bareGit(t, bare, "ls-tree", "-r", "--full-tree", tree)
	if got != want {
		t.Fatalf("the written tree lists\n%s\nwant\n%s", got, want)
	}
}

// A reconstruction that disagrees with the provider is refused. This is the check
// that makes the approach safe: without it a controller bug would merge a tree
// nobody asked about, and the result would be published as a repair.
func TestWriteRefusesATreeTheFilesDoNotMake(t *testing.T) {
	dir, commit := workingRepository(t)
	wrong := strings.Repeat("0", 40)
	_, err := Write(t.TempDir(), []Commit{{Tree: wrong, Files: archiveOf(t, dir, commit)}})
	if err == nil {
		t.Fatal("a tree that does not match was written")
	}
	if !strings.Contains(err.Error(), wrong) {
		t.Errorf("the refusal does not name the tree that was claimed: %v", err)
	}
}

// An archive's paths are untrusted input. One that is absolute or climbs out of
// the repository must not be written, and must not change the tree it claims.
func TestWriteIgnoresAPathThatEscapesTheRepository(t *testing.T) {
	control := []File{{Path: "keep.txt", Mode: "100644", Content: []byte("keep\n")}}
	escaping := append([]File{
		{Path: "../escape.txt", Mode: "100644", Content: []byte("no\n")},
		{Path: "/absolute.txt", Mode: "100644", Content: []byte("no\n")},
	}, control...)

	plain := t.TempDir()
	ids, err := Write(plain, []Commit{{Files: control}})
	if err != nil {
		t.Fatal(err)
	}
	want := treeID(t, plain, ids[0])

	guarded := t.TempDir()
	ids, err = Write(guarded, []Commit{{Files: escaping, Tree: want}})
	if err != nil {
		t.Fatalf("writing with an escaping path: %v", err)
	}
	if got := treeID(t, guarded, ids[0]); got != want {
		t.Fatalf("the escaping paths changed the tree: %s is not %s", got, want)
	}
}

// The reconstructed repository is only worth anything if the worker can merge in
// it: this runs the real merge package against three commits this package wrote,
// and expects the conflict back with its three sides.
func TestTheWorkerMergesARepositoryThisPackageBuilt(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	git(t, dir, "config", "user.email", "test@example.test")
	git(t, dir, "config", "user.name", "Test")
	writeFile := func(content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeFile("a\nb\nc\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "base")
	ancestor := git(t, dir, "rev-parse", "HEAD")
	git(t, dir, "checkout", "-qb", "side")
	writeFile("a\nB\nc\n")
	git(t, dir, "commit", "-qam", "side")
	head := git(t, dir, "rev-parse", "HEAD")
	git(t, dir, "checkout", "-q", "main")
	writeFile("a\nb\nC\n")
	git(t, dir, "commit", "-qam", "main")
	base := git(t, dir, "rev-parse", "HEAD")

	bare := t.TempDir()
	when := time.Unix(1700000000, 0)
	// The ancestor exists before the sides can name it, which is why the graph is
	// written in two passes, exactly as the controller will.
	ids, err := Write(bare, []Commit{{
		Tree:    git(t, dir, "rev-parse", ancestor+"^{tree}"),
		When:    when,
		Message: "base",
		Files:   archiveOf(t, dir, ancestor),
	}})
	if err != nil {
		t.Fatal(err)
	}
	ancestorID := ids[0]
	ids, err = Write(bare, []Commit{
		{
			Tree:    git(t, dir, "rev-parse", base+"^{tree}"),
			When:    when,
			Message: "main",
			Parents: []string{ancestorID},
			Files:   archiveOf(t, dir, base),
		},
		{
			Tree:    git(t, dir, "rev-parse", head+"^{tree}"),
			When:    when,
			Message: "side",
			Parents: []string{ancestorID},
			Files:   archiveOf(t, dir, head),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	baseID, headID := ids[0], ids[1]

	result, err := gitmerge.Merge(context.Background(), gitmerge.Request{Dir: bare, Ancestor: ancestorID, Base: baseID, Head: headID})
	if err != nil {
		t.Fatalf("the worker refused a repository this package built: %v", err)
	}
	if len(result.Conflicts) != 1 {
		t.Fatalf("merging the reconstruction gave %+v", result)
	}
	conflict := result.Conflicts[0]
	if conflict.Path != "file.txt" || conflict.Ancestor != "a\nb\nc\n" || conflict.Base != "a\nb\nC\n" || conflict.Head != "a\nB\nc\n" {
		t.Fatalf("conflict %+v", conflict)
	}
	if result.Tree == "" {
		t.Fatal("the merge produced no tree")
	}
}
