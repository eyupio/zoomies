package launcher

import "testing"

func TestAnArchitectureWithNoLauncherGetsNone(t *testing.T) {
	if Exe("riscv64") != nil {
		t.Fatal("a launcher was returned for an architecture nobody builds")
	}
}
