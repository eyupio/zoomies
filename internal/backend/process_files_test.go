package backend

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The runner never writes to its own program files, so sharing inodes between
// runners is safe -- and it is what turns a 400MB copy per runner into a
// handful of directory entries.
func TestCloneTreeSharesInodesWhereItCan(t *testing.T) {
	requirePOSIX(t)
	src, dst := t.TempDir(), filepath.Join(t.TempDir(), "clone")

	if err := os.MkdirAll(filepath.Join(src, "bin"), 0o750); err != nil {
		t.Fatal(err)
	}
	prog := filepath.Join(src, "bin", "Runner.Listener")
	if err := os.WriteFile(prog, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("Runner.Listener", filepath.Join(src, "bin", "run.sh")); err != nil {
		t.Fatal(err)
	}

	if err := cloneTree(src, dst); err != nil {
		t.Fatalf("cloneTree: %v", err)
	}

	cloned := filepath.Join(dst, "bin", "Runner.Listener")
	body, err := os.ReadFile(cloned)
	if err != nil {
		t.Fatalf("reading the clone: %v", err)
	}
	if !strings.Contains(string(body), "exit 0") {
		t.Fatalf("the clone does not carry the file's contents: %q", body)
	}
	// The executable bit has to survive, or the runner cannot be started.
	info, err := os.Stat(cloned)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("mode = %v, want the executable bit kept", info.Mode())
	}
	// A hard link is the point: same inode, so the copy costs a directory
	// entry rather than the file.
	if !sameFile(t, prog, cloned) {
		t.Error("the cloned program file is a copy rather than a link")
	}

	link, err := os.Readlink(filepath.Join(dst, "bin", "run.sh"))
	if err != nil {
		t.Fatalf("the symlink did not survive the clone: %v", err)
	}
	if link != "Runner.Listener" {
		t.Fatalf("symlink target = %q, want it copied as-is", link)
	}

	// Cloning over an existing tree replaces the symlink rather than failing on
	// it, which is what makes a retried create work.
	if err := cloneTree(src, dst); err != nil {
		t.Fatalf("cloneTree over an existing tree: %v", err)
	}
}

// copyFile is the fallback for a filesystem that will not link -- a separate
// mount, or one with no hard links at all -- so it has to produce the same
// file, mode included.
func TestCopyFileKeepsContentsAndMode(t *testing.T) {
	requirePOSIX(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "prog")
	if err := os.WriteFile(src, []byte("the program"), 0o755); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(dir, "copy")
	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile: %v", err)
	}
	body, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "the program" {
		t.Fatalf("copy = %q", body)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %v, want 0755", info.Mode().Perm())
	}

	// A source that is not there is reported rather than producing an empty
	// file that would look like a runner with no program in it.
	if err := copyFile(filepath.Join(dir, "absent"), filepath.Join(dir, "out")); err == nil {
		t.Error("copying a file that does not exist succeeded")
	}
	// So is a destination that cannot be written.
	if err := copyFile(src, filepath.Join(dir, "no-such-dir", "out")); err == nil {
		t.Error("copying into a directory that does not exist succeeded")
	}
}

// A restarted agent recognises its runners by what it wrote beside them, so
// the metadata has to round-trip -- and a directory with none is ErrNotFound
// rather than a zero-valued runner the agent would then act on.
func TestRunnerMetadataRoundTrips(t *testing.T) {
	dir := t.TempDir()
	created := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	want := processMeta{
		Name: "zoomies-abc", RunnerID: "run_1", PoolID: "pool_1", PoolName: "hosted",
		Version: "2.320.0", Ephemeral: true, PID: 4242, CreatedAt: created, StartedAt: created,
	}
	if err := writeMeta(dir, want); err != nil {
		t.Fatalf("writeMeta: %v", err)
	}
	got, err := readMeta(dir)
	if err != nil {
		t.Fatalf("readMeta: %v", err)
	}
	if got.Name != want.Name || got.RunnerID != want.RunnerID || got.PID != want.PID || !got.Ephemeral {
		t.Fatalf("metadata round-tripped wrong: %+v", got)
	}
	if !got.CreatedAt.Equal(created) {
		t.Fatalf("created_at = %v, want %v", got.CreatedAt, created)
	}

	if _, err := readMeta(t.TempDir()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("readMeta on an empty directory = %v, want ErrNotFound", err)
	}

	// Writing into a directory that is not there is an error naming the
	// directory, because the alternative is a runner the agent forgets.
	if err := writeMeta(filepath.Join(dir, "absent"), want); err == nil {
		t.Error("metadata was written into a directory that does not exist")
	}
}

// Asking for the log of a runner that has not written one yet is not an error:
// a viewer who opens the page the instant a runner starts should see an empty
// file filling up, not a failure.
func TestLogsOfARunnerThatHasWrittenNothingYet(t *testing.T) {
	requireUnix(t)
	b, root := newStubProcessBackend(t)
	dir := filepath.Join(root, "runners", "zoomies-quiet")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}

	rc, err := b.Logs(context.Background(), Handle(dir), LogOptions{})
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	defer rc.Close()
	body, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("reading the log: %v", err)
	}
	if len(body) != 0 {
		t.Fatalf("a runner that has written nothing produced %q", body)
	}

	// A directory that was never a runner is a different thing, and says so.
	if _, err := b.Logs(context.Background(), Handle(filepath.Join(root, "nowhere")), LogOptions{}); err == nil {
		t.Error("logs were offered for a runner directory that does not exist")
	} else if !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

// Tailing hands back the last few lines rather than the whole file, which is
// what makes a page open quickly on a runner that has been going for an hour.
func TestLogsTailReturnsOnlyTheLastLines(t *testing.T) {
	requireUnix(t)
	b, root := newStubProcessBackend(t)
	dir := filepath.Join(root, "runners", "zoomies-chatty")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	var lines strings.Builder
	for i := range 100 {
		lines.WriteString("line ")
		lines.WriteString(string(rune('0' + i%10)))
		lines.WriteString("\n")
	}
	lines.WriteString("the last line\n")
	if err := os.WriteFile(filepath.Join(dir, runnerLogFile), []byte(lines.String()), 0o640); err != nil {
		t.Fatal(err)
	}

	rc, err := b.Logs(context.Background(), Handle(dir), LogOptions{Tail: 3})
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	defer rc.Close()
	body, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("reading the log: %v", err)
	}
	if !strings.Contains(string(body), "the last line") {
		t.Fatalf("the tail did not reach the end of the file:\n%s", body)
	}
	if got := strings.Count(string(body), "\n"); got > 4 {
		t.Fatalf("the tail returned %d lines, want about 3", got)
	}
}

// Sampling memory is free on Linux and nowhere else, so everywhere else it
// reports nothing rather than guessing -- and a handle that is not a running
// runner reports nothing rather than failing the heartbeat that asked.
func TestStatsSaysNothingRatherThanGuessing(t *testing.T) {
	b, root := newStubProcessBackend(t)

	got, err := b.Stats(context.Background(), Handle(filepath.Join(root, "runners", "nowhere")))
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if got.MemoryBytes != 0 || got.CPUPercent != 0 {
		t.Fatalf("Stats for a runner that is not there = %+v, want nothing", got)
	}

	// CPU is left at zero even on Linux: sampling it properly means holding
	// state between calls, and the host's own tooling does that better.
	if runtime.GOOS == "linux" && got.CPUPercent != 0 {
		t.Fatalf("CPU = %v, want zero", got.CPUPercent)
	}
}

func sameFile(t *testing.T, a, b string) bool {
	t.Helper()
	ai, err := os.Stat(a)
	if err != nil {
		t.Fatal(err)
	}
	bi, err := os.Stat(b)
	if err != nil {
		t.Fatal(err)
	}
	return os.SameFile(ai, bi)
}
