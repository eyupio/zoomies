//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// The installer scenario's prerequisites, which are deliberately not the
// GitHub scenario's.
//
// Setting a host up touches nothing on GitHub: it makes a service account,
// writes /etc/zoomies, seals a key and creates an administrator. So this
// scenario needs Docker and a Linux binary and nothing else, and it is worth
// keeping that true -- it means a gate with no GitHub App still runs the half
// of the product that a first-time operator meets first.

// installerImage is the throwaway host the installer is let loose on. It has
// to carry useradd, which the Debian base does.
func installerImage() string {
	return orDefault(os.Getenv("ZOOMIES_E2E_INSTALLER_IMAGE"), "debian:stable-slim")
}

// installerPreflight checks what this scenario needs before it creates
// anything, in the same spirit as preflight: a prerequisite found halfway
// through is a container left running on somebody's machine.
func installerPreflight() []string {
	var missing []string

	if _, err := exec.LookPath("docker"); err != nil {
		missing = append(missing, "docker is not on PATH: the installer is run inside a throwaway container")
	} else if out, err := exec.Command("docker", "info", "--format", "{{.ServerVersion}}").CombinedOutput(); err != nil {
		missing = append(missing, fmt.Sprintf("the Docker daemon is not reachable: %s", strings.TrimSpace(string(out))))
	}

	bin := builtBinary()
	info, err := os.Stat(bin)
	switch {
	case err != nil:
		missing = append(missing, "the zoomies binary is not built; run `make build` first")
	case info.IsDir():
		missing = append(missing, bin+" is a directory, not the zoomies binary")
	default:
		// The binary is bind-mounted into a Linux container and executed
		// there, so a darwin or windows build cannot run however healthy it
		// looks on this host. Saying so here beats "exec format error" from
		// inside a container sixty seconds later.
		if !isLinuxELF(bin) {
			missing = append(missing, fmt.Sprintf(
				"%s is not a Linux executable, so it cannot run inside the container this scenario installs into; "+
					"build one with GOOS=linux go build -o zoomies ./cmd/zoomies", bin))
		}
	}
	return missing
}

// isLinuxELF reports whether the file starts with the ELF magic. It is a
// cheap, dependency-free check for "this will run in a Linux container",
// which is the only thing the scenario needs to know.
func isLinuxELF(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var magic [4]byte
	if _, err := f.Read(magic[:]); err != nil {
		return false
	}
	return magic == [4]byte{0x7f, 'E', 'L', 'F'}
}
