package backend

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/eyupio/zoomies/internal/store"
)

const (
	// mergeWorkerMount is where the prepared workspace appears in the worker.
	mergeWorkerMount = "/work"
	// mergeWorkerAnswer is the file the worker writes its one JSON answer to.
	// It is a file rather than stdout so Git's own warnings on stderr cannot
	// be mistaken for the answer.
	mergeWorkerAnswer = "answer.json"
	// maxMergeWorkerAnswer bounds what the backend will read back. It matches
	// the worker's own response budget, with room for the newline it writes.
	maxMergeWorkerAnswer = (8 << 20) + 1024
	// The merge is a few files of JSON and a Git tree, not a build: it gets a
	// fixed slice so it cannot take a runner's share of the host.
	mergeWorkerCPUs     = 1
	mergeWorkerMemoryMB = 1024
)

var _ MergeWorkerRunner = (*DockerBackend)(nil)

// RunMergeWorker runs the bounded Git worker image against one prepared
// workspace and returns the answer it wrote.
//
// The container has its own fixed name, no network and only the workspace
// mounted, and it is removed before and after the run so a redelivery does not
// inherit a leftover container. The image's own user stays in place: the
// workspace directories are made writable by the caller, and Git is told the
// checkout is safe even though a different uid owns it.
func (b *DockerBackend) RunMergeWorker(ctx context.Context, image, workspace string) ([]byte, error) {
	if _, ok := ctx.Deadline(); !ok {
		// ContainerWait refuses an unbounded wait, and a worker that does not
		// answer must not hold the caller for ever.
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 10*time.Minute)
		defer cancel()
	}
	if image == "" {
		return nil, errors.New("backend: no merge worker image to run")
	}
	clean := filepath.Clean(workspace)
	if workspace == "" || !filepath.IsAbs(clean) {
		return nil, fmt.Errorf("backend: merge workspace %q is not an absolute path", workspace)
	}
	if info, err := os.Stat(clean); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("backend: merge workspace %s is not a readable directory", workspace)
	}

	ref, _, _, _, err := b.prepareImage(ctx, image, store.PullIfNotPresent)
	if err != nil {
		return nil, err
	}
	name := containerName("zoomies-merge-" + filepath.Base(clean))
	cfg := ContainerCreateRequest{
		Image:      ref,
		Entrypoint: []string{"/usr/local/bin/zoomies"},
		Cmd:        []string{"git-worker", "--request", mergeWorkerMount + "/request.json", "--answer", mergeWorkerMount + "/" + mergeWorkerAnswer},
		Labels:     map[string]string{LabelRole: "merge-worker"},
		// Attach is not needed for the answer, but it keeps the daemon from
		// discarding output that explains a crash.
		AttachStdout: true,
		AttachStderr: true,
		HostConfig: &HostConfig{
			LogConfig:     runnerLogConfig(b.fl),
			RestartPolicy: RestartPolicy{Name: "no"},
			CapDrop:       append([]string(nil), b.fl.capDrop...),
			NanoCPUs:      nanoCPUs(mergeWorkerCPUs),
			Memory:        mergeWorkerMemoryMB * 1024 * 1024,
			MemorySwap:    mergeWorkerMemoryMB * 1024 * 1024,
			// No network: the worker holds no credential and has nothing to
			// fetch, so a route out is only a way to reach something else.
			NetworkMode: "none",
			Binds:       []string{clean + ":" + mergeWorkerMount + b.fl.mountSuffix},
		},
	}

	cleanup := func() {
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if err := b.removeContainer(rctx, name); err != nil && !errors.Is(err, ErrNotFound) {
			b.log.Warn("could not remove the merge worker container", "container", name, "error", err)
		}
	}
	cleanup()
	id, err := b.api.ContainerCreate(ctx, name, cfg)
	if err != nil {
		return nil, fmt.Errorf("backend: creating the merge worker container: %w", err)
	}
	defer cleanup()
	if err := b.api.ContainerStart(ctx, id); err != nil {
		return nil, fmt.Errorf("backend: starting the merge worker container: %w", err)
	}
	code, waitErr := b.api.ContainerWait(ctx, id)

	answer, readErr := readMergeAnswer(clean)
	if readErr == nil && len(answer) > 0 {
		// A conflicted merge is a successful answer even when the worker exits
		// non-zero, so the answer wins over the exit status.
		return answer, nil
	}
	tail := b.mergeWorkerTail(id, ctx)
	switch {
	case waitErr != nil:
		return nil, fmt.Errorf("backend: the merge worker did not finish: %w%s", waitErr, tail)
	case code != 0:
		return nil, fmt.Errorf("backend: the merge worker exited with status %d%s", code, tail)
	case readErr != nil:
		return nil, fmt.Errorf("backend: reading the merge worker's answer: %w", readErr)
	default:
		return nil, fmt.Errorf("backend: the merge worker wrote no answer%s", tail)
	}
}

// readMergeAnswer reads the one file the worker was told to write, under the
// same budget the worker itself uses.
func readMergeAnswer(dir string) ([]byte, error) {
	file, err := os.Open(filepath.Join(dir, mergeWorkerAnswer))
	if errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, maxMergeWorkerAnswer+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxMergeWorkerAnswer {
		return nil, fmt.Errorf("the answer is larger than %d bytes", maxMergeWorkerAnswer)
	}
	return body, nil
}

// mergeWorkerTail returns the last lines of a worker that produced no answer,
// which is what says whether it was the image, the mount or Git that failed.
func (b *DockerBackend) mergeWorkerTail(id string, ctx context.Context) string {
	lctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	rc, err := b.api.ContainerLogs(lctx, id, LogQuery{Stdout: true, Stderr: true, Tail: 20})
	if err != nil {
		return ""
	}
	defer rc.Close()
	tail, _ := io.ReadAll(io.LimitReader(rc, 16<<10))
	if len(tail) == 0 {
		return ""
	}
	return ": " + string(tail)
}
