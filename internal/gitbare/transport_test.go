package gitbare

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/gitmerge"
)

// The transport's whole promise: a repository packed on one host, unpacked on
// another, merges to the same answer. Anything less makes the agent's copy a
// different incident from the controller's.
func TestPackAndUnpackKeepTheRepositoryMergeable(t *testing.T) {
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

	controller := t.TempDir()
	ids, err := Write(controller, []Commit{
		{Tree: git(t, dir, "rev-parse", ancestor+"^{tree}"), Files: archiveOf(t, dir, ancestor), Message: "base"},
	})
	if err != nil {
		t.Fatal(err)
	}
	sides, err := Write(controller, []Commit{
		{Tree: git(t, dir, "rev-parse", base+"^{tree}"), Files: archiveOf(t, dir, base), Message: "main", Parents: []string{ids[0]}},
		{Tree: git(t, dir, "rev-parse", head+"^{tree}"), Files: archiveOf(t, dir, head), Message: "side", Parents: []string{ids[0]}},
	})
	if err != nil {
		t.Fatal(err)
	}

	packed, err := Pack(context.Background(), controller, 0)
	if err != nil {
		t.Fatalf("packing the repository: %v", err)
	}
	agent := t.TempDir()
	if err := Unpack(bytes.NewReader(packed), agent, Budget{}); err != nil {
		t.Fatalf("unpacking the repository: %v", err)
	}

	result, err := gitmerge.Merge(context.Background(), gitmerge.Request{
		Dir:      agent,
		Ancestor: ids[0],
		Base:     sides[0],
		Head:     sides[1],
	})
	if err != nil {
		t.Fatalf("the worker refused the unpacked repository: %v", err)
	}
	if len(result.Conflicts) != 1 || result.Conflicts[0].Path != "file.txt" {
		t.Fatalf("merging the unpacked repository gave %+v", result)
	}

	// Loose objects have to stay read-only, or Git will not trust them when the
	// worker writes the merge's own objects beside them.
	object := firstLooseObject(t, agent)
	info, err := os.Stat(object)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o222 != 0 {
		t.Fatalf("%s is writable (%v); Git refuses a loose object that can be rewritten", object, info.Mode().Perm())
	}
}

// firstLooseObject finds one object file in a repository this package wrote.
func firstLooseObject(t *testing.T, repo string) string {
	t.Helper()
	var found string
	err := filepath.WalkDir(filepath.Join(repo, "objects"), func(path string, entry os.DirEntry, err error) error {
		if err != nil || found != "" || entry.IsDir() {
			return nil
		}
		found = path
		return nil
	})
	if err != nil || found == "" {
		t.Fatalf("no loose object found in %s: %v", repo, err)
	}
	return found
}

// A packed repository over its budget is refused rather than half written, and
// so is one whose entry names somewhere outside the directory.
func TestUnpackRefusesWhatItCannotWrite(t *testing.T) {
	packedRepository := func() []byte {
		repo := t.TempDir()
		if _, err := Write(repo, []Commit{{Files: []File{{Path: "a.txt", Content: []byte("a\n")}}}}); err != nil {
			t.Fatal(err)
		}
		packed, err := Pack(context.Background(), repo, 0)
		if err != nil {
			t.Fatal(err)
		}
		return packed
	}

	if err := Unpack(bytes.NewReader(packedRepository()), t.TempDir(), Budget{Bytes: 8}); !errors.Is(err, ErrRepository) {
		t.Fatalf("a repository over its byte budget answered %v", err)
	}
	if err := Unpack(strings.NewReader("not a tarball"), t.TempDir(), Budget{}); !errors.Is(err, ErrRepository) {
		t.Fatalf("a body that is not a repository answered %v", err)
	}

	// One entry that climbs out, in a tarball that is otherwise a repository.
	var buf bytes.Buffer
	zipper := gzip.NewWriter(&buf)
	writer := tar.NewWriter(zipper)
	_ = writer.WriteHeader(&tar.Header{Name: "../escape.txt", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg})
	_, _ = writer.Write([]byte("x"))
	_ = writer.Close()
	_ = zipper.Close()
	if err := Unpack(bytes.NewReader(buf.Bytes()), t.TempDir(), Budget{}); !errors.Is(err, ErrRepository) {
		t.Fatalf("a path that climbs out answered %v", err)
	}
}

// Packing refuses a symlink, because a repository this package wrote has none and
// one that does would point at whatever the packer's host had there.
func TestPackRefusesASymlink(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "HEAD"), []byte("ref: refs/heads/none\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", filepath.Join(repo, "config")); err != nil {
		t.Fatal(err)
	}
	if _, err := Pack(context.Background(), repo, 0); !errors.Is(err, ErrRepository) {
		t.Fatalf("packing a symlink answered %v", err)
	}
}

// A caller that only needs to know a repository can be packed must not have to
// hold the tarball to find out.
func TestPackToWritesWhatPackReturnsAndRefusesWhatPackRefuses(t *testing.T) {
	dir := t.TempDir()
	if _, err := Write(dir, []Commit{{Files: []File{{Path: "a.txt", Content: []byte(strings.Repeat("a", 2048))}}}}); err != nil {
		t.Fatal(err)
	}
	packed, err := Pack(context.Background(), dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	var streamed bytes.Buffer
	if err := PackTo(context.Background(), dir, 0, &streamed); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(packed, streamed.Bytes()) {
		t.Fatal("PackTo wrote different bytes from Pack")
	}
	if err := PackTo(context.Background(), dir, 16, io.Discard); !errors.Is(err, ErrRepository) {
		t.Fatalf("a repository over the budget answered %v, want ErrRepository", err)
	}
}
