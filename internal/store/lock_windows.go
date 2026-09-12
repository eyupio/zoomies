//go:build windows

package store

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// lockFile takes an exclusive byte-range lock on the whole file, which Windows
// releases when the handle closes -- including when the process dies, however
// it dies. That is the same property flock gives on POSIX, and the reason the
// lock is a kernel object rather than a pid written into a file: a controller
// killed by the service manager must not leave a lock behind that an operator
// has to find and delete before the service will start again.
func lockFile(path string) (func() error, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("opening the database lock at %s: %w", path, err)
	}
	h := windows.Handle(f.Fd())
	ol := new(windows.Overlapped)
	if err := windows.LockFileEx(h, windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, ol); err != nil {
		f.Close()
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return nil, ErrLocked
		}
		return nil, fmt.Errorf("locking %s: %w", path, err)
	}
	return func() error {
		err := windows.UnlockFileEx(h, 0, 1, 0, ol)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		return err
	}, nil
}
