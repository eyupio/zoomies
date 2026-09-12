//go:build unix || windows

package store

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The lock is what stops a second controller on the same host, and it has to
// hold between processes rather than only between goroutines -- a lock the
// same process can take twice would pass a test and protect nothing.
func TestTheDatabaseLockIsRefusedToASecondProcess(t *testing.T) {
	db := filepath.Join(t.TempDir(), "zoomies.db")

	release, err := Lock(db)
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}

	// A second process, because flock is per open file description: this
	// process asking again would be granted it. The helper fails unless the
	// lock is refused, so this asserts on its exit status.
	helper := exec.Command(os.Args[0], "-test.run=TestLockHelperProcess", "-test.v")
	helper.Env = append(os.Environ(), "ZOOMIES_TEST_LOCK_DB="+db)
	out, err := helper.CombinedOutput()
	if err != nil {
		t.Fatalf("the helper did not see the lock held:\n%s", out)
	}
	// And prove the helper actually ran rather than skipping itself, which is
	// the way this test lies to you.
	if !strings.Contains(string(out), "TestLockHelperProcess") || strings.Contains(string(out), "SKIP") {
		t.Fatalf("the helper process did not run its assertion:\n%s", out)
	}

	if err := release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	// And once released, the next controller starts.
	again, err := Lock(db)
	if err != nil {
		t.Fatalf("the lock was not released: %v", err)
	}
	if err := again(); err != nil {
		t.Fatal(err)
	}
}

// TestLockHelperProcess is the second process. It fails unless the lock is
// already held, which is what the test above is asserting.
func TestLockHelperProcess(t *testing.T) {
	db := os.Getenv("ZOOMIES_TEST_LOCK_DB")
	if db == "" {
		t.Skip("not the helper process")
	}
	if _, err := Lock(db); !errors.Is(err, ErrLocked) {
		t.Fatalf("Lock in a second process = %v, want ErrLocked", err)
	}
}

// An in-memory database belongs to one process by construction, so locking one
// would only make every test that opens a store contend with the others.
func TestAnInMemoryDatabaseIsNotLocked(t *testing.T) {
	release, err := Lock(":memory:")
	if err != nil {
		t.Fatalf("Lock(:memory:) = %v", err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	// Two of them at once, as the test suite does constantly.
	a, err := Lock(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	b, err := Lock(":memory:")
	if err != nil {
		t.Fatalf("a second in-memory store was refused: %v", err)
	}
	_ = a()
	_ = b()
}

// The lock file sits beside the database, because the database is what two
// controllers contend for.
func TestTheLockSitsBesideTheDatabase(t *testing.T) {
	if got, want := LockPath("/var/lib/zoomies/zoomies.db"), "/var/lib/zoomies/zoomies.db.lock"; got != want {
		t.Errorf("LockPath = %q, want %q", got, want)
	}
}
