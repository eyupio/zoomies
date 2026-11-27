//go:build windows

package silent

import (
	"os/exec"
	"testing"
)

// The one property the whole launcher rests on: the child is created with no
// console. A real window cannot be seen from a test, so this checks the flags
// Windows is given; see docs/hosts-and-pools.md ("WSL2 scheduled tasks") for how the rest is checked.
func TestHideAsksWindowsForNoConsoleAndNoWindow(t *testing.T) {
	c := exec.Command("wsl.exe")
	Hide(c)
	if c.SysProcAttr == nil || c.SysProcAttr.CreationFlags&0x08000000 == 0 || !c.SysProcAttr.HideWindow {
		t.Fatalf("SysProcAttr = %+v", c.SysProcAttr)
	}
}

func TestEnsureRunsAHiddenProcessAndReturnsItsExitCode(t *testing.T) {
	code, err := Exec{Path: `C:\Windows\System32\cmd.exe`}.Run(t.Context(), "/c", "exit", "7")
	if err != nil || code != 7 {
		t.Fatalf("code=%d err=%v", code, err)
	}
}
