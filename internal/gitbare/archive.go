package gitbare

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"strings"
)

// ErrArchive says an archive cannot be read as a commit's tree: it is not the
// shape the writer takes, or it costs more than the budget it was given.
var ErrArchive = errors.New("gitbare: this archive cannot be read")

// The default budget for one archive. A repository past these is one a repair
// should decline rather than spend minutes of a controller's memory on: the
// numbers are generous for a source tree and small next to a runner image.
const (
	DefaultArchiveFiles = 200_000
	DefaultArchiveBytes = 256 << 20
)

// Budget bounds one archive's cost. A zero field takes its default.
type Budget struct {
	Files int
	Bytes int64
}

// ArchiveOptions says how to read the provider's tarball.
type ArchiveOptions struct {
	// StripTopLevel drops the first path element of every entry. A provider's
	// archive of a commit is wrapped in one directory named after the
	// repository and the commit, and that directory is not part of the tree.
	StripTopLevel bool
	Budget        Budget
}

// ReadTarGz reads one commit's gzipped tar archive and returns the files it
// holds, ready for Write.
//
// It refuses rather than repairs. A path that is absolute, climbs with "..", or
// is not a regular file, a directory or a symlink is an error, not a file the
// caller never knew was dropped: the tree check in Write would catch a dropped
// file eventually, but a reader that says why is worth more than one that does.
func ReadTarGz(r io.Reader, opts ArchiveOptions) ([]File, error) {
	budget := opts.Budget
	if budget.Files <= 0 {
		budget.Files = DefaultArchiveFiles
	}
	if budget.Bytes <= 0 {
		budget.Bytes = DefaultArchiveBytes
	}

	uncompressed, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("%w: not a gzipped archive: %v", ErrArchive, err)
	}
	defer uncompressed.Close()

	reader := tar.NewReader(uncompressed)
	files := make([]File, 0, 64)
	var total int64
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrArchive, err)
		}
		name := header.Name
		if opts.StripTopLevel {
			_, rest, found := strings.Cut(name, "/")
			if !found {
				// The wrapper directory itself.
				continue
			}
			name = rest
		}
		if name == "" {
			continue
		}
		if header.Typeflag == tar.TypeDir {
			continue
		}
		clean, inside := insideRepository(name)
		if !inside {
			return nil, fmt.Errorf("%w: %q is not a path inside the repository", ErrArchive, name)
		}
		name = clean

		mode, ok := archiveMode(header)
		if !ok {
			return nil, fmt.Errorf("%w: %s is a %s, which is not part of a tree a model may edit", ErrArchive, name, describeType(header.Typeflag))
		}

		content := []byte(nil)
		if header.Typeflag == tar.TypeSymlink {
			content = []byte(header.Linkname)
		} else {
			if header.Size < 0 || header.Size > budget.Bytes-total {
				return nil, fmt.Errorf("%w: %s takes the archive past %d bytes", ErrArchive, name, budget.Bytes)
			}
			content, err = io.ReadAll(io.LimitReader(reader, budget.Bytes-total+1))
			if err != nil {
				return nil, fmt.Errorf("%w: reading %s: %v", ErrArchive, name, err)
			}
			if int64(len(content)) != header.Size {
				return nil, fmt.Errorf("%w: %s is shorter than its header says", ErrArchive, name)
			}
		}

		total += int64(len(content))
		if len(files) >= budget.Files {
			return nil, fmt.Errorf("%w: more than %d files", ErrArchive, budget.Files)
		}
		files = append(files, File{Path: name, Mode: mode, Content: content})
	}
	return files, nil
}

// typeRegOld is the pre-POSIX marker for a regular file, which some archives
// still write. It is spelled here rather than taken from archive/tar because
// that constant is deprecated: the marker did not stop existing, the name for it
// did.
const typeRegOld = 0

// archiveMode maps a tar header to the Git mode the file it describes has.
//
// The two that matter are executable and not, and the symlink is its own mode
// because a symlink's content is a path rather than a file. Anything else is
// refused by the caller.
func archiveMode(header *tar.Header) (string, bool) {
	switch header.Typeflag {
	case tar.TypeReg, typeRegOld:
		if header.Mode&0o111 != 0 {
			return "100755", true
		}
		return "100644", true
	case tar.TypeSymlink:
		return "120000", true
	default:
		return "", false
	}
}

func describeType(flag byte) string {
	switch flag {
	case tar.TypeLink:
		return "hard link"
	case tar.TypeChar:
		return "character device"
	case tar.TypeBlock:
		return "block device"
	case tar.TypeFifo:
		return "named pipe"
	default:
		return "entry this reader does not write"
	}
}
