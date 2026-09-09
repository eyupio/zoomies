package store

import (
	"fmt"
	"path/filepath"
	"strings"
)

// LockPath is the file a controller takes an exclusive lock on before it opens
// the database. It sits beside the database rather than in the state directory
// as such, because the database is the thing two controllers actually contend
// for: a second one pointed at the same file through a different state
// directory is the same disaster.
func LockPath(dbPath string) string {
	return filepath.Join(filepath.Dir(dbPath), filepath.Base(dbPath)+".lock")
}

// Lock takes an exclusive, non-blocking lock for the database at path and
// returns the release. It answers ErrLocked when another process holds it.
//
// Non-blocking on purpose: a controller that waited would look like a
// controller that is starting slowly, and the operator would be left watching
// a process that never binds its port and never says why.
//
// An in-memory database is nobody else's, so it is not locked.
func Lock(dbPath string) (func() error, error) {
	if dbPath == "" || strings.HasPrefix(dbPath, ":memory:") || strings.Contains(dbPath, "mode=memory") {
		return func() error { return nil }, nil
	}
	return lockFile(LockPath(dbPath))
}

// ErrLocked is another process holding the database lock on this host. It is
// distinct from a permission or disk failure, because the answer differs: one
// is "a controller is already running here", the other is "this process cannot
// write where it was told to".
var ErrLocked = fmt.Errorf("another process holds the database lock")
