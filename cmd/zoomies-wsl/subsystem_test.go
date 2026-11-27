package main

import (
	"debug/pe"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The launcher is only silent because it is a GUI-subsystem program: Windows
// gives a console-subsystem one a console, and Windows Terminal turns that into
// a window. This builds it the way the Makefile does and reads the subsystem
// out of the PE header, so a build that loses -H windowsgui fails here, on any
// platform, without needing a desktop to look at.
func TestTheLauncherIsBuiltForTheGUISubsystem(t *testing.T) {
	if testing.Short() {
		t.Skip("cross-compiles a program")
	}
	for _, arch := range []string{"amd64", "arm64"} {
		out := filepath.Join(t.TempDir(), "zoomies-wsl.exe")
		cmd := exec.Command("go", "build", "-ldflags", "-H windowsgui", "-o", out, ".")
		cmd.Env = append(os.Environ(), "GOOS=windows", "GOARCH="+arch, "CGO_ENABLED=0")
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s: %v\n%s", arch, err, b)
		}
		f, err := pe.Open(out)
		if err != nil {
			t.Fatal(err)
		}
		var sub uint16
		switch h := f.OptionalHeader.(type) {
		case *pe.OptionalHeader64:
			sub = h.Subsystem
		case *pe.OptionalHeader32:
			sub = h.Subsystem
		}
		f.Close()
		if sub != pe.IMAGE_SUBSYSTEM_WINDOWS_GUI {
			t.Fatalf("%s: subsystem %d, want the GUI subsystem (%d)", arch, sub, pe.IMAGE_SUBSYSTEM_WINDOWS_GUI)
		}
	}
}
