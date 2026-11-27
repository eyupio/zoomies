//go:build !windows

package silent

import "os/exec"

// Hide does nothing where there are no console windows to hide.
func Hide(*exec.Cmd) {}
