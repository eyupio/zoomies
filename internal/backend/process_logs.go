package backend

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// Bound native diagnostics without putting a pipe between runner and agent:
// a runner must keep logging after its agent restarts. Copy-truncate preserves
// the inherited descriptor. A small concurrent-write gap is possible, so this
// is diagnostic retention, never the authoritative GitHub workflow log.
func (b *ProcessBackend) PruneLogs(ctx context.Context, limit int64) error {
	entries, err := os.ReadDir(filepath.Join(b.root, runnersDirName))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(b.root, runnersDirName, e.Name())
		if _, err := readMeta(dir); err != nil {
			continue
		}
		if err := boundLog(filepath.Join(dir, runnerLogFile), limit); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}
func boundLog(path string, limit int64) error {
	if limit <= 0 {
		return nil
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() <= limit {
		return nil
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(info, opened) {
		return nil
	}
	archive, err := os.CreateTemp(filepath.Dir(path), "log-archive-*")
	if err != nil {
		return err
	}
	defer os.Remove(archive.Name())
	_, err = io.CopyN(archive, io.NewSectionReader(f, info.Size()-limit, limit), limit)
	closeErr := archive.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Rename(archive.Name(), path+".previous"); err != nil {
		return err
	}
	return f.Truncate(0)
}
