//go:build unix

package store

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// lockFile takes a BSD advisory lock, which the kernel drops when the process
// exits however it exits. That is the property that matters here: a controller
// killed with SIGKILL must not leave a lock behind that an operator has to
// find and remove before the service will start again.
func lockFile(path string) (func() error, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("opening the database lock at %s: %w", path, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrLocked
		}
		return nil, fmt.Errorf("locking %s: %w", path, err)
	}
	return func() error {
		// Unlock before closing so the error, if there is one, is reportable;
		// closing the descriptor would release it anyway.
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		return err
	}, nil
}
