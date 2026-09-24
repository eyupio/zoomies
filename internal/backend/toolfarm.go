package backend

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// RunnerToolCacheSharedMount is where a pool's kept tool cache is mounted in a
// runner, read-only. The runner's AGENT_TOOLSDIRECTORY is a folder of its own
// at RunnerToolCacheMount, which links to every complete version in here.
//
// The kept cache used to be mounted writable, and that was a race: when two
// runners asked for a version it lacked, actions/tool-cache in each deleted
// the folder, copied the files in and wrote the .complete marker, with no
// lock between them. One run of this repository's own CI left a marker beside
// half a Go, and every job after it trusted the marker and failed. Read-only
// also means a job can no longer replace the toolchain the next job runs. The
// fill container is now the only thing that writes to the kept cache; a
// version it has not put there is downloaded into the runner's own folder and
// thrown away with the runner.
const RunnerToolCacheSharedMount = "/opt/zoomies-tools-shared"

// LabelToolFarm records the host folder holding a runner's own tool cache, so
// removing the runner removes it too.
const LabelToolFarm = LabelPrefix + "toolfarm"

// toolFarmDir is the host folder a runner's own tool cache is made in: beside
// the kept caches, in the shared folder the daemon can already bind from.
func toolFarmDir(sharedDir, runner string) string {
	return filepath.Join(sharedDir, "cache", "tool-runs", containerName(runner))
}

// buildToolFarm makes farm a tool cache the runner can write to, holding a
// link to each version in shared whose install finished. A version is
// tool/version/arch plus a tool/version/arch.complete marker; one without its
// marker is a fill still under way, or one that failed, and is left out
// rather than handed to a job half-written. The links point at the read-only
// mount, which is where they resolve inside the runner.
func buildToolFarm(shared, farm string) error {
	if err := os.RemoveAll(farm); err != nil {
		return err
	}
	if err := mkdirOpen(farm); err != nil {
		return err
	}
	markers, err := filepath.Glob(filepath.Join(shared, "*", "*", "*.complete"))
	if err != nil {
		return err
	}
	for _, marker := range markers {
		rel, err := filepath.Rel(shared, strings.TrimSuffix(marker, ".complete"))
		if err != nil {
			return err
		}
		if fi, err := os.Stat(filepath.Join(shared, rel)); err != nil || !fi.IsDir() {
			continue
		}
		parts := strings.Split(rel, string(filepath.Separator))
		if err := mkdirOpen(filepath.Join(farm, parts[0])); err != nil {
			return err
		}
		if err := mkdirOpen(filepath.Join(farm, parts[0], parts[1])); err != nil {
			return err
		}
		link := filepath.Join(farm, rel)
		if err := os.Symlink(RunnerToolCacheSharedMount+"/"+filepath.ToSlash(rel), link); err != nil {
			return err
		}
		if err := os.WriteFile(link+".complete", nil, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// mkdirOpen creates a folder the runner can add to, whatever its uid: the same
// reasoning as ensureRunnerWritableDir, for a folder that is only ever ours.
func mkdirOpen(dir string) error {
	if err := os.Mkdir(dir, 0o750); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	if err := os.Chmod(dir, 0o777); err != nil {
		return fmt.Errorf("opening %s to the runner: %w", dir, err)
	}
	return nil
}
