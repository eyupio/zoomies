package gitbare

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type tarEntry struct {
	name     string
	mode     int64
	kind     byte
	content  []byte
	linkname string
}

func fileEntry(name string, mode int64, content string) tarEntry {
	return tarEntry{name: name, mode: mode, kind: tar.TypeReg, content: []byte(content)}
}

// archive builds a gzipped tar in the shape a provider serves: one wrapper
// directory holding the commit's tree.
func archive(t *testing.T, entries ...tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zipper := gzip.NewWriter(&buf)
	writer := tar.NewWriter(zipper)
	for _, entry := range entries {
		header := &tar.Header{
			Name:     entry.name,
			Mode:     entry.mode,
			Typeflag: entry.kind,
			Linkname: entry.linkname,
		}
		if entry.kind == tar.TypeReg {
			header.Size = int64(len(entry.content))
		}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if header.Size > 0 {
			if _, err := writer.Write(entry.content); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zipper.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// The reader's whole job: a provider's tarball of one commit becomes the file
// list that hashes to the tree Git wrote for that commit.
func TestReadTarGzReproducesTheTreeOfARealCommit(t *testing.T) {
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
	if err := os.Symlink("README.md", filepath.Join(dir, "link.md")); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "one")
	tree := git(t, dir, "rev-parse", "HEAD^{tree}")

	payload := archive(t,
		tarEntry{name: "acme-zoomies-abc123/", mode: 0o755, kind: tar.TypeDir},
		fileEntry("acme-zoomies-abc123/README.md", 0o644, "hello\n"),
		fileEntry("acme-zoomies-abc123/bin/run.sh", 0o755, "#!/bin/sh\necho hi\n"),
		fileEntry("acme-zoomies-abc123/src/app/main.go", 0o644, "package main\n"),
		tarEntry{name: "acme-zoomies-abc123/link.md", mode: 0o777, kind: tar.TypeSymlink, linkname: "README.md"},
	)

	files, err := ReadTarGz(bytes.NewReader(payload), ArchiveOptions{StripTopLevel: true})
	if err != nil {
		t.Fatalf("reading the archive: %v", err)
	}
	bare := t.TempDir()
	ids, err := Write(bare, []Commit{{Tree: tree, Files: files}})
	if err != nil {
		t.Fatalf("writing what the archive described: %v", err)
	}
	if got := treeID(t, bare, ids[0]); got != tree {
		t.Fatalf("the archive's files hash to tree %s, want %s", got, tree)
	}
}

// Every refusal names the entry, because the operator's next question is which
// file in their repository caused it.
func TestReadTarGzRefusesWhatItCannotWrite(t *testing.T) {
	for _, tc := range []struct {
		name  string
		entry tarEntry
		why   string
	}{
		{"an absolute path", fileEntry("/etc/passwd", 0o644, "x\n"), "/etc/passwd"},
		{"a path that climbs out", fileEntry("../escape.txt", 0o644, "x\n"), "escape.txt"},
		{"a hard link", tarEntry{name: "hard", kind: tar.TypeLink, linkname: "README.md"}, "hard link"},
		{"a device", tarEntry{name: "null", kind: tar.TypeChar}, "character device"},
		{"a named pipe", tarEntry{name: "pipe", kind: tar.TypeFifo}, "named pipe"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files, err := ReadTarGz(bytes.NewReader(archive(t, tc.entry)), ArchiveOptions{})
			if err == nil {
				t.Fatalf("%s was read as %+v", tc.name, files)
			}
			if !errors.Is(err, ErrArchive) {
				t.Fatalf("%s answered %v, want ErrArchive", tc.name, err)
			}
			if !strings.Contains(err.Error(), tc.why) {
				t.Errorf("the refusal does not name %q: %v", tc.why, err)
			}
		})
	}
}

// The budgets are the only thing between a hostile archive and the controller's
// memory, so each one is checked at the boundary rather than trusted.
func TestReadTarGzStopsAtItsBudget(t *testing.T) {
	twoFiles := archive(t, fileEntry("a.txt", 0o644, "a\n"), fileEntry("b.txt", 0o644, "b\n"))
	if _, err := ReadTarGz(bytes.NewReader(twoFiles), ArchiveOptions{Budget: Budget{Files: 1}}); !errors.Is(err, ErrArchive) {
		t.Fatalf("a second file over a budget of one answered %v", err)
	}
	big := archive(t, fileEntry("a.txt", 0o644, strings.Repeat("a", 4096)))
	if _, err := ReadTarGz(bytes.NewReader(big), ArchiveOptions{Budget: Budget{Bytes: 1024}}); !errors.Is(err, ErrArchive) {
		t.Fatalf("a file over the byte budget answered %v", err)
	}
	// A file exactly at the budget is fine: the boundary is the byte past it.
	exact := archive(t, fileEntry("a.txt", 0o644, "abc"))
	if _, err := ReadTarGz(bytes.NewReader(exact), ArchiveOptions{Budget: Budget{Bytes: 3}}); err != nil {
		t.Fatalf("a file exactly at the byte budget was refused: %v", err)
	}
}

// A body that is not the archive the provider promised is refused rather than
// decompressed into nonsense: a proxy's error page is gzipped often enough.
func TestReadTarGzRefusesWhatIsNotAnArchive(t *testing.T) {
	for _, body := range []string{"", "not a gzip stream", `{"error": "Not Found"}`} {
		if _, err := ReadTarGz(strings.NewReader(body), ArchiveOptions{}); !errors.Is(err, ErrArchive) {
			t.Fatalf("%q answered %v, want ErrArchive", body, err)
		}
	}
	var buf bytes.Buffer
	zipper := gzip.NewWriter(&buf)
	_, _ = zipper.Write([]byte("<html>denied</html>"))
	_ = zipper.Close()
	if _, err := ReadTarGz(bytes.NewReader(buf.Bytes()), ArchiveOptions{}); !errors.Is(err, ErrArchive) {
		t.Fatalf("a gzipped error page answered %v, want ErrArchive", err)
	}
}

// Without the strip the wrapper directory would become a directory in the commit,
// which is the bug that makes a reconstruction disagree with the provider in a
// way nobody can read.
func TestReadTarGzKeepsTheWrapperWhenNotAskedToStrip(t *testing.T) {
	payload := archive(t, fileEntry("wrapper/keep.txt", 0o644, "keep\n"))
	files, err := ReadTarGz(bytes.NewReader(payload), ArchiveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "wrapper/keep.txt" {
		t.Fatalf("files %+v", files)
	}
}
