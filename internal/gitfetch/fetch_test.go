package gitfetch

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/gitbare"
	"github.com/eyupio/zoomies/internal/gitmerge"
)

// git runs one Git command in a working repository.
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

// repoSource answers from a real repository, which is what the controller's
// GitHub client will do from an archive and a commit.
type repoSource struct {
	t   *testing.T
	dir string
	// wrong makes the source answer about a commit nobody asked for, which is
	// the mistake that would merge a different incident.
	wrong bool
	// brokenTree claims a tree id the files cannot make.
	brokenTree bool
}

func (s repoSource) Commit(_ context.Context, sha string) (Commit, error) {
	s.t.Helper()
	files := archive(s.t, s.dir, sha)
	commit := Commit{
		SHA:     sha,
		Tree:    git(s.t, s.dir, "rev-parse", sha+"^{tree}"),
		Parents: strings.Fields(git(s.t, s.dir, "rev-list", "--parents", "-n", "1", sha)),
		Files:   files,
	}
	if s.wrong {
		commit.SHA = strings.Repeat("f", 40)
	}
	if s.brokenTree {
		commit.Tree = strings.Repeat("0", 40)
	}
	return commit, nil
}

// archive reads a commit the way a provider's archive presents it.
func archive(t *testing.T, dir, sha string) []gitbare.File {
	t.Helper()
	listing := git(t, dir, "ls-tree", "-r", "-z", "--full-tree", sha)
	var files []gitbare.File
	for _, entry := range strings.Split(listing, "\x00") {
		if entry == "" {
			continue
		}
		meta, name, ok := strings.Cut(entry, "\t")
		if !ok {
			t.Fatalf("ls-tree entry %q is malformed", entry)
		}
		fields := strings.Fields(meta)
		cmd := exec.Command("git", "cat-file", "blob", fields[2])
		cmd.Dir = dir
		content, err := cmd.Output()
		if err != nil {
			t.Fatalf("cat-file blob %s: %v", fields[2], err)
		}
		files = append(files, gitbare.File{Path: name, Mode: fields[0], Content: content})
	}
	return files
}

// conflictingRepository builds the incident: one file both sides edit.
func conflictingRepository(t *testing.T) (dir string, pin Pin) {
	t.Helper()
	dir = t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	git(t, dir, "config", "user.email", "test@example.test")
	git(t, dir, "config", "user.name", "Test")
	write := func(content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a\nb\nc\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "base")
	ancestor := git(t, dir, "rev-parse", "HEAD")
	git(t, dir, "checkout", "-qb", "side")
	write("a\nB\nc\n")
	git(t, dir, "commit", "-qam", "side")
	head := git(t, dir, "rev-parse", "HEAD")
	git(t, dir, "checkout", "-q", "main")
	write("a\nb\nC\n")
	git(t, dir, "commit", "-qam", "main")
	base := git(t, dir, "rev-parse", "HEAD")
	return dir, Pin{Ancestor: ancestor, Base: base, Head: head}
}

// The point of the package: three pinned commits, fetched one at a time, become a
// repository the worker merges and answers with the real conflict.
func TestBuildGivesTheWorkerAConflictItCanRead(t *testing.T) {
	dir, pin := conflictingRepository(t)
	repo, err := Build(context.Background(), t.TempDir(), repoSource{t: t, dir: dir}, pin)
	if err != nil {
		t.Fatalf("building the repository: %v", err)
	}
	t.Cleanup(func() { _ = Remove(repo.Dir) })

	result, err := gitmerge.Merge(context.Background(), gitmerge.Request{
		Dir:      repo.Dir,
		Ancestor: repo.Ancestor,
		Base:     repo.Base,
		Head:     repo.Head,
	})
	if err != nil {
		t.Fatalf("the worker refused the repository this package built: %v", err)
	}
	if len(result.Conflicts) != 1 {
		t.Fatalf("merging the fetch gave %+v", result)
	}
	conflict := result.Conflicts[0]
	if conflict.Path != "file.txt" || conflict.Ancestor != "a\nb\nc\n" || conflict.Base != "a\nb\nC\n" || conflict.Head != "a\nB\nc\n" {
		t.Fatalf("conflict %+v", conflict)
	}
	if repo.Ancestor == repo.Base || repo.Ancestor == repo.Head || repo.Base == repo.Head {
		t.Fatalf("the three ids are not distinct: %+v", repo)
	}
}

// A source that answers about another commit is a different incident, and merging
// it while the watch holds evidence about this one is the failure this refusal
// exists for.
func TestBuildRefusesAnAnswerAboutAnotherCommit(t *testing.T) {
	dir, pin := conflictingRepository(t)
	_, err := Build(context.Background(), t.TempDir(), repoSource{t: t, dir: dir, wrong: true}, pin)
	if !errors.Is(err, ErrWrongCommit) {
		t.Fatalf("a mismatched commit answered %v", err)
	}
}

// The provider's tree id is the only check that the reconstruction is the commit
// the operator is looking at, so a source that reports a different one must stop
// the fetch rather than produce a plausible repository.
func TestBuildRefusesWhenTheFilesDoNotMakeTheTree(t *testing.T) {
	dir, pin := conflictingRepository(t)
	_, err := Build(context.Background(), t.TempDir(), repoSource{t: t, dir: dir, brokenTree: true}, pin)
	if err == nil {
		t.Fatal("a tree the files do not make was fetched")
	}
	if !strings.Contains(err.Error(), "reconstructed tree") {
		t.Errorf("the refusal does not say what disagreed: %v", err)
	}
}

// A pin that is not three commit ids is refused before any source is asked, so a
// bad watch row costs nothing and says so plainly.
func TestBuildRefusesAPinThatIsNotCommitIds(t *testing.T) {
	for _, pin := range []Pin{
		{Ancestor: "main", Base: strings.Repeat("b", 40), Head: strings.Repeat("c", 40)},
		{Ancestor: strings.Repeat("a", 40), Base: "", Head: strings.Repeat("c", 40)},
		{Ancestor: strings.Repeat("a", 40), Base: strings.Repeat("b", 39), Head: strings.Repeat("c", 40)},
	} {
		if _, err := Build(context.Background(), t.TempDir(), repoSource{t: t}, pin); !errors.Is(err, ErrUnpinned) {
			t.Fatalf("pin %+v answered %v", pin, err)
		}
	}
}

// A built repository is read-only inside, and removing it is what the controller
// does on every path out of a repair, successful or not.
func TestRemoveDeletesABuiltRepository(t *testing.T) {
	dir, pin := conflictingRepository(t)
	repo, err := Build(context.Background(), t.TempDir(), repoSource{t: t, dir: dir}, pin)
	if err != nil {
		t.Fatal(err)
	}
	if err := Remove(repo.Dir); err != nil {
		t.Fatalf("removing the repository: %v", err)
	}
	if _, err := os.Stat(repo.Dir); !os.IsNotExist(err) {
		t.Fatalf("the repository is still there: %v", err)
	}
}

// lookupFromRepo is a Lookup over a real repository, which is what the
// controller's GitHub client will be: the commit's tree and parents from one
// read, and the files from another.
type lookupFromRepo struct {
	t   *testing.T
	dir string
}

func (l lookupFromRepo) Commit(_ context.Context, sha string) (string, []string, error) {
	l.t.Helper()
	tree := git(l.t, l.dir, "rev-parse", sha+"^{tree}")
	parents := strings.Fields(git(l.t, l.dir, "rev-list", "--parents", "-n", "1", sha))
	if len(parents) < 2 {
		return tree, nil, nil
	}
	return tree, parents[1:], nil
}

func (l lookupFromRepo) Files(_ context.Context, sha string) ([]gitbare.File, error) {
	return archive(l.t, l.dir, sha), nil
}

// The adapter is the whole of the controller's side: a commit's tree from one
// read and its files from another, and Build refuses when they disagree.
func TestNewSourceFeedsBuildFromALookup(t *testing.T) {
	dir, pin := conflictingRepository(t)
	repo, err := Build(context.Background(), t.TempDir(), NewSource(lookupFromRepo{t: t, dir: dir}), pin)
	if err != nil {
		t.Fatalf("building from a lookup: %v", err)
	}
	t.Cleanup(func() { _ = Remove(repo.Dir) })

	result, err := gitmerge.Merge(context.Background(), gitmerge.Request{
		Dir:      repo.Dir,
		Ancestor: repo.Ancestor,
		Base:     repo.Base,
		Head:     repo.Head,
	})
	if err != nil {
		t.Fatalf("the worker refused what the lookup produced: %v", err)
	}
	if len(result.Conflicts) != 1 || result.Conflicts[0].Path != "file.txt" {
		t.Fatalf("merging what the lookup produced gave %+v", result)
	}
}

// A lookup whose two halves disagree is the failure the pair exists to catch: the
// commit says one tree and the archive hashes to another.
func TestNewSourceRefusesALookupThatDisagrees(t *testing.T) {
	dir, pin := conflictingRepository(t)
	source := NewSource(disagreeingLookup{lookupFromRepo{t: t, dir: dir}})
	if _, err := Build(context.Background(), t.TempDir(), source, pin); err == nil {
		t.Fatal("a lookup whose tree does not match its files was built")
	}
}

type disagreeingLookup struct{ lookupFromRepo }

func (d disagreeingLookup) Commit(ctx context.Context, sha string) (string, []string, error) {
	_, parents, err := d.lookupFromRepo.Commit(ctx, sha)
	return strings.Repeat("0", 40), parents, err
}
