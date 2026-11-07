package gitbare

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// MaxRepositoryBytes bounds a packed repository. It is larger than an archive's
// budget because a repository carries three trees plus the objects a merge
// writes, and it is small next to the host it runs on.
const MaxRepositoryBytes = 512 << 20

// ErrRepository says a packed repository cannot be written where it was asked to
// go: it is not a tarball, it costs more than its budget, or it names a path
// outside the directory.
var ErrRepository = errors.New("gitbare: this is not a repository this worker will unpack")

// Pack writes the repository at dir as a gzipped tarball, which is what crosses
// to the agent that runs the worker.
//
// Decision 0018 says why the repository travels as a tarball rather than inside a
// task: a task is a bounded command, and a queue that carries megabytes is a queue
// nobody can reason about.
func Pack(ctx context.Context, dir string, maxBytes int64) ([]byte, error) {
	var buffer bytes.Buffer
	if err := PackTo(ctx, dir, maxBytes, &buffer); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// PackTo is Pack written to w as it goes. A caller that only needs to know the
// repository can be packed passes io.Discard and holds none of it, where Pack
// would keep the whole tarball in memory to throw it away.
func PackTo(ctx context.Context, dir string, maxBytes int64, w io.Writer) error {
	if maxBytes <= 0 {
		maxBytes = MaxRepositoryBytes
	}
	zipper := gzip.NewWriter(w)
	writer := tar.NewWriter(zipper)
	var total int64
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		name, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return relErr
		}
		if name == "." {
			return nil
		}
		name = filepath.ToSlash(name)
		info, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}
		switch {
		case entry.IsDir():
			return writer.WriteHeader(&tar.Header{
				Name:     name + "/",
				Mode:     0o755,
				Typeflag: tar.TypeDir,
			})
		case info.Mode()&os.ModeSymlink != 0:
			// A repository this package wrote holds none, and a symlink in one
			// would let a packed tree point outside the directory it unpacks to.
			return fmt.Errorf("%w: %s is a symlink", ErrRepository, name)
		case !info.Mode().IsRegular():
			return fmt.Errorf("%w: %s is not a regular file", ErrRepository, name)
		}
		if info.Size() > maxBytes-total {
			return fmt.Errorf("%w: the repository is larger than %d bytes", ErrRepository, maxBytes)
		}
		if err := writer.WriteHeader(&tar.Header{
			Name:     name,
			Mode:     int64(info.Mode().Perm()),
			Size:     info.Size(),
			Typeflag: tar.TypeReg,
		}); err != nil {
			return err
		}
		file, openErr := os.Open(path)
		if openErr != nil {
			return openErr
		}
		defer file.Close()
		written, copyErr := io.Copy(writer, io.LimitReader(file, info.Size()))
		total += written
		return copyErr
	})
	if err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return zipper.Close()
}

// Unpack writes a packed repository into dir, creating it.
//
// Every refusal is a refusal, not a repair: a path that is absolute or climbs
// with "..", an entry that is not a regular file or a directory, and a tarball
// past its budget all fail before anything outside dir could be written.
func Unpack(r io.Reader, dir string, budget Budget) error {
	if budget.Files <= 0 {
		budget.Files = DefaultArchiveFiles
	}
	if budget.Bytes <= 0 {
		budget.Bytes = MaxRepositoryBytes
	}
	uncompressed, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("%w: not a gzipped archive: %v", ErrRepository, err)
	}
	defer uncompressed.Close()

	reader := tar.NewReader(uncompressed)
	var files int
	var total int64
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("%w: %v", ErrRepository, err)
		}
		if header.Typeflag == tar.TypeDir {
			clean := strings.TrimSuffix(strings.ReplaceAll(header.Name, "\\", "/"), "/")
			if clean == "" || clean == "." {
				continue
			}
			trimmed, inside := insideRepository(clean)
			if !inside {
				return fmt.Errorf("%w: %q is not a path inside the repository", ErrRepository, header.Name)
			}
			dirTarget := filepath.Join(dir, filepath.FromSlash(trimmed))
			// insideRepository has already refused "..", so this is the same
			// rule restated where the join happens: a second, local check is
			// what stops a later edit to the helper from becoming a Zip Slip.
			if !strings.HasPrefix(dirTarget, filepath.Clean(dir)+string(os.PathSeparator)) {
				return fmt.Errorf("%w: %q is not a path inside the repository", ErrRepository, header.Name)
			}
			if err := os.MkdirAll(dirTarget, 0o755); err != nil {
				return err
			}
			continue
		}
		name, ok := insideRepository(header.Name)
		if !ok {
			return fmt.Errorf("%w: %q is not a path inside the repository", ErrRepository, header.Name)
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != typeRegOld {
			return fmt.Errorf("%w: %s is not a regular file", ErrRepository, name)
		}
		if files >= budget.Files {
			return fmt.Errorf("%w: more than %d files", ErrRepository, budget.Files)
		}
		if header.Size < 0 || header.Size > budget.Bytes-total {
			return fmt.Errorf("%w: %s takes the repository past %d bytes", ErrRepository, name, budget.Bytes)
		}
		target := filepath.Join(dir, filepath.FromSlash(name))
		if !strings.HasPrefix(target, filepath.Clean(dir)+string(os.PathSeparator)) {
			return fmt.Errorf("%w: %q is not a path inside the repository", ErrRepository, header.Name)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		content, err := io.ReadAll(io.LimitReader(reader, header.Size))
		if err != nil {
			return fmt.Errorf("%w: reading %s: %v", ErrRepository, name, err)
		}
		total += int64(len(content))
		files++
		mode := os.FileMode(header.Mode).Perm()
		if mode == 0 {
			mode = 0o644
		}
		if err := os.WriteFile(target, content, mode); err != nil {
			return err
		}
		// WriteFile applies the umask, and Git refuses a loose object that is not
		// read-only, so the mode is set again after the file exists.
		if err := os.Chmod(target, mode); err != nil {
			return err
		}
	}
	return nil
}
