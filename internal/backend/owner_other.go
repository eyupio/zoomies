//go:build !unix

package backend

import "io/fs"

// fileOwner has no answer off unix, where a folder has no owning uid for a
// Linux runner to be refused by.
func fileOwner(fs.FileInfo) (uid, gid int, ok bool) { return 0, 0, false }
