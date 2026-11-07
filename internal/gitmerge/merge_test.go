package gitmerge

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func repo(t testing.TB) string {
	t.Helper()
	dir := t.TempDir()
	mustGit(t, dir, "init", "-q")
	mustGit(t, dir, "config", "user.email", "test@example.test")
	mustGit(t, dir, "config", "user.name", "Test")
	return dir
}

func mustGit(t testing.TB, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func commit(t testing.TB, dir, message string) string {
	t.Helper()
	mustGit(t, dir, "add", "-A")
	mustGit(t, dir, "commit", "-qm", message)
	return mustGit(t, dir, "rev-parse", "HEAD")
}

func write(t testing.TB, dir, name string, content []byte) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMergeReturnsACleanTree(t *testing.T) {
	dir := repo(t)
	write(t, dir, "base.txt", []byte("base\n"))
	ancestor := commit(t, dir, "base")
	mustGit(t, dir, "checkout", "-qb", "side")
	write(t, dir, "side.txt", []byte("side\n"))
	side := commit(t, dir, "side")
	mustGit(t, dir, "checkout", "-q", "-")
	write(t, dir, "base.txt", []byte("base\nmain\n"))
	base := commit(t, dir, "main")
	got, err := Merge(context.Background(), Request{Dir: dir, Ancestor: ancestor, Base: base, Head: side})
	if err != nil {
		t.Fatal(err)
	}
	if got.Tree == "" || len(got.Conflicts) != 0 || len(got.Unsupported) != 0 {
		t.Fatalf("merge %+v", got)
	}
}

func TestMergeReturnsSupportedTextConflict(t *testing.T) {
	dir := repo(t)
	write(t, dir, "file.txt", []byte("a\nb\nc\n"))
	ancestor := commit(t, dir, "base")
	mustGit(t, dir, "checkout", "-qb", "side")
	write(t, dir, "file.txt", []byte("a\nB\nc\n"))
	side := commit(t, dir, "side")
	mustGit(t, dir, "checkout", "-q", "-")
	write(t, dir, "file.txt", []byte("a\nb\nC\n"))
	base := commit(t, dir, "main")
	got, err := Merge(context.Background(), Request{Dir: dir, Ancestor: ancestor, Base: base, Head: side})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Conflicts) != 1 || got.Conflicts[0].Path != "file.txt" ||
		got.Conflicts[0].Ancestor != "a\nb\nc\n" || got.Conflicts[0].Base != "a\nb\nC\n" || got.Conflicts[0].Head != "a\nB\nc\n" {
		t.Fatalf("conflict %+v", got)
	}
}

func TestMergeRefusesBinaryConflict(t *testing.T) {
	dir := repo(t)
	write(t, dir, "file.bin", []byte{0, 1, 2, 3})
	ancestor := commit(t, dir, "base")
	mustGit(t, dir, "checkout", "-qb", "side")
	write(t, dir, "file.bin", []byte{0, 1, 9, 3})
	side := commit(t, dir, "side")
	mustGit(t, dir, "checkout", "-q", "-")
	write(t, dir, "file.bin", []byte{0, 8, 2, 3})
	base := commit(t, dir, "main")
	got, err := Merge(context.Background(), Request{Dir: dir, Ancestor: ancestor, Base: base, Head: side})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Unsupported) != 1 || !strings.Contains(got.Unsupported[0].Reason, "binary") {
		t.Fatalf("binary conflict %+v", got)
	}
}

func TestMergeRefusesDeleteModify(t *testing.T) {
	dir := repo(t)
	write(t, dir, "file.txt", []byte("a\n"))
	ancestor := commit(t, dir, "base")
	mustGit(t, dir, "checkout", "-qb", "side")
	write(t, dir, "file.txt", []byte("side\n"))
	side := commit(t, dir, "side")
	mustGit(t, dir, "checkout", "-q", "-")
	if err := os.Remove(filepath.Join(dir, "file.txt")); err != nil {
		t.Fatal(err)
	}
	base := commit(t, dir, "main deleted")
	got, err := Merge(context.Background(), Request{Dir: dir, Ancestor: ancestor, Base: base, Head: side})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Unsupported) != 1 || !strings.Contains(got.Unsupported[0].Reason, "delete") {
		t.Fatalf("delete/modify %+v", got)
	}
}

func TestMergeRefusesADifferentMergeBase(t *testing.T) {
	dir := repo(t)
	write(t, dir, "base.txt", []byte("base\n"))
	ancestor := commit(t, dir, "base")
	mustGit(t, dir, "checkout", "-qb", "side")
	write(t, dir, "side.txt", []byte("side\n"))
	side := commit(t, dir, "side")
	mustGit(t, dir, "checkout", "-q", "-")
	write(t, dir, "base.txt", []byte("main\n"))
	base := commit(t, dir, "main")
	_, err := Merge(context.Background(), Request{Dir: dir, Ancestor: side, Base: base, Head: ancestor})
	if !errors.Is(err, ErrMergeBaseMismatch) {
		t.Fatalf("wrong merge base answered %v", err)
	}
}

func TestMergeRefusesAnExternalMergeDriver(t *testing.T) {
	dir := repo(t)
	mustGit(t, dir, "config", "merge.evil.driver", "cat")
	_, err := Merge(context.Background(), Request{Dir: dir, Ancestor: strings.Repeat("a", 40), Base: strings.Repeat("b", 40), Head: strings.Repeat("c", 40)})
	if !errors.Is(err, ErrUnsafeRepository) {
		t.Fatalf("merge driver answered %v", err)
	}
}

func TestMergeRefusesInvalidRequest(t *testing.T) {
	if _, err := Merge(context.Background(), Request{Dir: ".", Ancestor: "not-a-sha", Base: strings.Repeat("b", 40), Head: strings.Repeat("c", 40)}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("invalid request answered %v", err)
	}
}

func TestMergeRefusesContentOverItsBudget(t *testing.T) {
	dir := repo(t)
	write(t, dir, "file.txt", []byte(strings.Repeat("a", 64)+"\n"))
	ancestor := commit(t, dir, "base")
	mustGit(t, dir, "checkout", "-qb", "side")
	write(t, dir, "file.txt", []byte(strings.Repeat("a", 64)+"side\n"))
	side := commit(t, dir, "side")
	mustGit(t, dir, "checkout", "-q", "-")
	write(t, dir, "file.txt", []byte(strings.Repeat("a", 64)+"base\n"))
	base := commit(t, dir, "main")
	got, err := Merge(context.Background(), Request{Dir: dir, Ancestor: ancestor, Base: base, Head: side, MaxTotal: 8})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Unsupported) != 1 || !strings.Contains(got.Unsupported[0].Reason, "budget") {
		t.Fatalf("budget %+v", got)
	}
}

// The controller has to build the two-parent commit on GitHub, where the merged
// tree object does not exist. The worker therefore reports every file the merged
// tree has that the pull request head does not, deletions included.
func TestMergeReportsTheFilesTheMergedTreeChanges(t *testing.T) {
	dir := repo(t)
	write(t, dir, "common.txt", []byte("one\n"))
	write(t, dir, "gone.txt", []byte("remove me\n"))
	ancestor := commit(t, dir, "base")
	mustGit(t, dir, "checkout", "-qb", "side")
	write(t, dir, "side.txt", []byte("only on the pull request\n"))
	side := commit(t, dir, "side")
	mustGit(t, dir, "checkout", "-q", "-")
	write(t, dir, "common.txt", []byte("one\ntwo\n"))
	write(t, dir, "added-by-base.txt", []byte("from the target branch\n"))
	if err := os.Remove(filepath.Join(dir, "gone.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(dir, "added-by-base.txt"), 0o755); err != nil {
		t.Fatal(err)
	}
	base := commit(t, dir, "target moved")

	got, err := Merge(context.Background(), Request{Dir: dir, Ancestor: ancestor, Base: base, Head: side})
	if err != nil {
		t.Fatal(err)
	}
	changes := map[string]Change{}
	for _, change := range got.Changes {
		changes[change.Path] = change
	}
	if len(changes) != 3 {
		t.Fatalf("changes = %+v, want common, gone and added", got.Changes)
	}
	if change := changes["common.txt"]; string(change.Content) != "one\ntwo\n" || change.Mode != "100644" || change.Deleted {
		t.Errorf("common.txt = %+v", change)
	}
	if change := changes["gone.txt"]; !change.Deleted || change.Path != "gone.txt" {
		t.Errorf("gone.txt = %+v", change)
	}
	if change := changes["added-by-base.txt"]; string(change.Content) != "from the target branch\n" || change.Mode != "100755" {
		t.Errorf("added-by-base.txt = %+v", change)
	}
	if _, ok := changes["side.txt"]; ok {
		t.Error("a file the pull request head already has was reported as a change")
	}
}

// Binary content crosses as bytes, because it is published exactly as the merge
// produced it and is never handed to the model.
func TestMergeReportsBinaryChangesWithoutDecodingThem(t *testing.T) {
	dir := repo(t)
	write(t, dir, "keep.txt", []byte("keep\n"))
	ancestor := commit(t, dir, "base")
	mustGit(t, dir, "checkout", "-qb", "side")
	write(t, dir, "side.txt", []byte("side\n"))
	side := commit(t, dir, "side")
	mustGit(t, dir, "checkout", "-q", "-")
	write(t, dir, "image.bin", []byte{0, 1, 2, 255})
	base := commit(t, dir, "target adds a binary")

	got, err := Merge(context.Background(), Request{Dir: dir, Ancestor: ancestor, Base: base, Head: side})
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range got.Changes {
		if change.Path == "image.bin" {
			if string(change.Content) != string([]byte{0, 1, 2, 255}) {
				t.Fatalf("binary content = %v", change.Content)
			}
			return
		}
	}
	t.Fatalf("the binary change is missing: %+v", got.Changes)
}

// Git's test switch simulates a checkout owned by the agent rather than the
// worker. The wrapper sets it after sanitisation, as a different uid would.
func TestMergeAcceptsOnlyItsPreparedRepositoryWithDifferentOwnership(t *testing.T) {
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	dir := repo(t)
	write(t, dir, "base.txt", []byte("base\n"))
	ancestor := commit(t, dir, "base")
	mustGit(t, dir, "checkout", "-qb", "side")
	write(t, dir, "side.txt", []byte("side\n"))
	head := commit(t, dir, "side")
	mustGit(t, dir, "checkout", "-q", "-")
	write(t, dir, "base.txt", []byte("main\n"))
	base := commit(t, dir, "main")
	wrapper := t.TempDir()
	script := "#!/bin/sh\nexport GIT_TEST_ASSUME_DIFFERENT_OWNER=1\nexec '" + realGit + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(wrapper, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", wrapper+string(os.PathListSeparator)+os.Getenv("PATH"))
	got, err := Merge(context.Background(), Request{Dir: dir, Ancestor: ancestor, Base: base, Head: head})
	if err != nil {
		t.Fatal(err)
	}
	if got.Tree == "" || len(got.Conflicts) != 0 {
		t.Fatalf("merge %+v", got)
	}
}

// "exit status 128" tells an operator nothing; Git's own sentence does.
func TestAGitFailureCarriesWhatGitSaid(t *testing.T) {
	dir := repo(t)
	write(t, dir, "a.txt", []byte("a\n"))
	commit(t, dir, "base")
	absent := strings.Repeat("a", 40)
	_, err := Merge(context.Background(), Request{Dir: dir, Ancestor: absent, Base: absent, Head: absent})
	if err == nil || !strings.Contains(err.Error(), "fatal") {
		t.Fatalf("error = %v, want one that carries git's message", err)
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("error %v no longer exposes the exit status", err)
	}
}

// The blobs of a merge come from one cat-file, so the budget has to be applied
// to what it reports, size first, rather than to bytes already read.
func TestReadBlobsRefusesAnObjectPastTheBudgetBeforeReadingIt(t *testing.T) {
	dir := repo(t)
	write(t, dir, "big.txt", []byte(strings.Repeat("x", 4096)))
	write(t, dir, "small.txt", []byte("s\n"))
	commit(t, dir, "files")
	big := mustGit(t, dir, "rev-parse", "HEAD:big.txt")
	small := mustGit(t, dir, "rev-parse", "HEAD:small.txt")
	got, err := readBlobs(context.Background(), dir, []string{small, big, small}, 1<<20)
	if err != nil || len(got) != 3 || string(got[0]) != "s\n" || len(got[1]) != 4096 || string(got[2]) != "s\n" {
		t.Fatalf("blobs = %d, %v", len(got), err)
	}
	if _, err := readBlobs(context.Background(), dir, []string{small, big}, 1024); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("an object past the budget answered %v, want ErrTooLarge", err)
	}
	if _, err := readBlobs(context.Background(), dir, []string{strings.Repeat("0", 40)}, 1024); err == nil {
		t.Fatal("a missing object was read")
	}
}
