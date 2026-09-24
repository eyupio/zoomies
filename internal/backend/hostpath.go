package backend

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path"
	"strconv"
	"strings"
)

// A containerised agent hands the host's daemon host paths: a runner's cache
// is bound from the shared folder by the path the agent sees it at, and the
// daemon resolves that path on the host. That is only the same folder when
// the agent's container mounts it from the host at that very path. When it
// does not -- a Compose file from before the shared folder existed -- the
// folder the agent creates is inside its own data volume, the daemon creates
// an empty one on the host owned by root, and a runner handed it as its tool
// cache cannot write to it: every setup action that downloads fails.
//
// So a containerised agent checks, and leaves the tool cache off with a
// reason rather than hand out a folder the daemon cannot see.

// containerMarkers are the files a container runtime leaves at the root of a
// container: Docker's and Podman's.
var containerMarkers = []string{"/.dockerenv", "/run/.containerenv"}

// runningInContainer is InContainer, a variable so a test can describe a
// container from a machine that is not one.
var runningInContainer = InContainer

// InContainer reports whether this process runs inside a container.
func InContainer() bool {
	for _, m := range containerMarkers {
		if _, err := os.Stat(m); err == nil {
			return true
		}
	}
	return false
}

// IsMountPoint reports whether dir is itself a mount point in this process's
// mount namespace -- a bind from the host, not a folder inside another mount.
// It is false where /proc cannot say, which is every system but Linux.
func IsMountPoint(dir string) bool {
	f, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return false
	}
	defer f.Close()
	return mountPoints(f)[path.Clean(dir)]
}

// mountPoints reads /proc/self/mountinfo: the fifth field of each line is
// the mount point, with spaces and the like written as octal escapes.
func mountPoints(r io.Reader) map[string]bool {
	out := map[string]bool{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 5 {
			continue
		}
		out[path.Clean(unescapeMount(f[4]))] = true
	}
	return out
}

func unescapeMount(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// SharedFolderProblem says why the shared folder at dir cannot be handed to
// the host's daemon, or "" when it can: this process is in a container and the
// folder is not mounted into it from the host at its own path.
func SharedFolderProblem(dir string, inContainer bool, isMount func(string) bool) string {
	if !inContainer || isMount(dir) {
		return ""
	}
	return fmt.Sprintf("the shared folder %s is not mounted into this container from the host, so the host's daemon cannot see what is in it; pools' tool caches are off here until it is. "+
		"Add `- %s:%s` to the zoomies service's volumes (or `--volume %s:%s` to its docker run), or run `zoomies upgrade --yes` on the host, which adds it", dir, dir, dir, dir, dir)
}
