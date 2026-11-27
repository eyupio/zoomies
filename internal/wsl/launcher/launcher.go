// Package launcher carries the Windows program the scheduled tasks run
// (cmd/zoomies-wsl), built for the GUI subsystem so it opens no console.
//
// It is a build product, like the UI: `make wsl-launcher` (which `make build`
// and `make dist` run) cross-compiles it into dist/, and the directory is not
// tracked. A build without it still compiles; Exe then returns nil and the
// scheduled tasks are left alone with a warning, rather than being rewritten
// to run something that would show a window.
package launcher

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Exe is the launcher for a Windows machine of the given GOARCH (the distro
// has the architecture of its host), or nil if this build does not carry one.
func Exe(goarch string) []byte {
	b, err := fs.ReadFile(dist, "dist/zoomies-wsl-"+goarch+".exe")
	if err != nil {
		return nil
	}
	return b
}
