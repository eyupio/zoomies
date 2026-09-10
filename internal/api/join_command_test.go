package api

import (
	"os/exec"
	"strings"
	"testing"
)

func TestJoinCommandsKeepURLsAndCredentialsLiteral(t *testing.T) {
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("a POSIX shell is not installed")
	}
	h := newHarness(t)
	for _, address := range []string{
		"https://zoomies.example/?first=1&second=2",
		"https://zoomies.example/$(printf injected)",
		"https://zoomies.example/it's-here",
	} {
		if msg := checkControllerURL(address); msg != "" {
			t.Fatal(msg)
		}
		const token = "join-token"
		// Neither curl nor the installer runs: these functions expose only
		// how the generated command is parsed into arguments by a real shell.
		script := "curl() { :; }; sh() { printf '%s\\n' \"$@\"; }; " + h.api.joinCommand(token, address)
		out, err := exec.CommandContext(t.Context(), shell, "-c", script).CombinedOutput()
		if err != nil {
			t.Fatalf("shell: %v: %s", err, out)
		}
		want := "\n--controller\n" + address + "\n--join-token\n" + token + "\n"
		if !strings.Contains(string(out), want) {
			t.Fatalf("URL %q changed during shell parsing: %s", address, out)
		}
	}
}
