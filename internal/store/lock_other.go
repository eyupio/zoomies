//go:build !unix && !windows

package store

// lockFile is the fallback for a platform this project does not release
// binaries for. There is no lock, and the controller lease in the database is
// the only thing standing between two controllers -- which is what catches the
// cross-machine case anyway, so the gap is narrower than it looks.
func lockFile(string) (func() error, error) {
	return func() error { return nil }, nil
}
