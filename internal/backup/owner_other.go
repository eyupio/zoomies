//go:build !unix

package backup

import "io/fs"

// fileOwner has no answer off unix, where a file has no owning uid and a copy
// is never handed on.
func fileOwner(fs.FileInfo) (uid, gid int, ok bool) { return 0, 0, false }
