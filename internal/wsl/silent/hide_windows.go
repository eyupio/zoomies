//go:build windows

package silent

import (
	"os/exec"
	"syscall"
)

// createNoWindow is CREATE_NO_WINDOW: the child gets no console at all, so
// there is nothing for Windows Terminal to take over. HideWindow adds
// STARTF_USESHOWWINDOW with SW_HIDE for a program that makes a window anyway.
const createNoWindow = 0x08000000

// Hide makes cmd start without a window.
func Hide(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}
