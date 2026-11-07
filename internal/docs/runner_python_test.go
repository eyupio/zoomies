package docs

import (
	"strings"
	"testing"
)

// GitHub's hosted runners have `python` as well as `python3`, so a workflow
// written for them says `shell: python` or `python script.py` without a thought.
// An image with only python3 fails those at their first line, and the repository
// that finds out is one that has just moved its workflows onto this fleet: the
// managed AI Context workflow did exactly that. The image provides the name, and
// its build proves that the name resolves to the interpreter python3 does.
func TestTheRunnerImageProvidesPythonAsWellAsPython3(t *testing.T) {
	toolchain := readDeployFile(t, "runner-toolchain.sh")
	if !strings.Contains(toolchain, `ln -s "$(command -v python3)" /usr/local/bin/python`) {
		t.Error("deploy/runner-toolchain.sh no longer links `python` to python3, so a hosted-runner workflow that calls `python` fails with \"command not found\"")
	}
	if !strings.Contains(toolchain, "if ! command -v python ") {
		t.Error("deploy/runner-toolchain.sh links `python` unconditionally, which would replace one a distribution package provides")
	}

	build := readDeployFile(t, "Dockerfile.runner")
	for _, want := range []string{
		"python --version;",
		`test "$(readlink -f "$(command -v python)")" = "$(readlink -f "$(command -v python3)")";`,
	} {
		if !strings.Contains(build, want) {
			t.Errorf("deploy/Dockerfile.runner does not check the image's python: missing %q", want)
		}
	}
}
