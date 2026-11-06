package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/eyupio/zoomies/internal/backend"
	"github.com/eyupio/zoomies/internal/gitbare"
	"github.com/eyupio/zoomies/internal/gitfetch"
	"github.com/eyupio/zoomies/internal/gitmerge"
	"github.com/eyupio/zoomies/internal/store"
)

// mergeWorkerTimeout bounds one worker run. The task's own lease is ten
// minutes and includes the image pull; a worker that has not answered by this
// point is not going to, and the controller must learn that before its lease.
const mergeWorkerTimeout = 8 * time.Minute

// handleMergeConflict pulls the prepared repository, runs the bounded Git
// worker against it and posts the one answer.
//
// The repository never comes from GitHub here: it is the tarball the controller
// built from three pinned commits, and the token that fetched them never leaves
// the controller. The workspace is removed whatever happened, including when
// the worker or the answer is refused.
func (a *Agent) handleMergeConflict(ctx context.Context, task Task) {
	prepared, err := a.tr.PullMergeRepository(ctx, task.MergeID)
	if err != nil {
		a.reportFailure(ctx, task, fmt.Sprintf("could not pull the repository for this merge: %v", err), store.FaultBackend)
		return
	}
	if prepared.Ancestor != task.MergeAncestor || prepared.Base != task.MergeBase || prepared.Head != task.MergeHead {
		a.reportFailure(ctx, task, "the controller's prepared repository names different commits from the merge task, so merging it would be a different incident", store.FaultConfig)
		return
	}

	workspace, err := os.MkdirTemp(a.opts.WorkDir, "zoomies-merge-")
	if err != nil {
		a.reportFailure(ctx, task, fmt.Sprintf("could not make a workspace for this merge: %v", err), store.FaultBackend)
		return
	}
	defer func() {
		if err := gitfetch.Remove(workspace); err != nil {
			a.log.Warn("could not remove a merge workspace", "workspace", workspace, "error", err)
		}
	}()
	repository := filepath.Join(workspace, "repository")
	if err := os.Mkdir(repository, 0o755); err != nil {
		a.reportFailure(ctx, task, fmt.Sprintf("could not make a merge repository directory: %v", err), store.FaultBackend)
		return
	}
	if err := gitbare.Unpack(bytes.NewReader(prepared.Repository), repository, gitbare.Budget{}); err != nil {
		a.reportFailure(ctx, task, fmt.Sprintf("the controller's merge repository could not be unpacked: %v", err), store.FaultConfig)
		return
	}
	if err := makeMergeWorkspaceWritable(workspace); err != nil {
		a.reportFailure(ctx, task, fmt.Sprintf("could not prepare the merge workspace for the worker: %v", err), store.FaultBackend)
		return
	}
	request, err := json.Marshal(gitmerge.WorkerRequest{
		Dir:      "/work/repository",
		Ancestor: task.MergeAncestor,
		Base:     task.MergeBase,
		Head:     task.MergeHead,
	})
	if err != nil {
		a.reportFailure(ctx, task, fmt.Sprintf("could not encode the merge request: %v", err), store.FaultConfig)
		return
	}
	if err := os.WriteFile(filepath.Join(workspace, "request.json"), request, 0o644); err != nil {
		a.reportFailure(ctx, task, fmt.Sprintf("could not write the merge request: %v", err), store.FaultBackend)
		return
	}

	kind := task.Backend
	if kind == "" {
		kind = a.opts.DefaultBackend
		b, err := a.opts.Backends.Get(kind)
		if _, capable := b.(backend.MergeWorkerRunner); err != nil || !capable {
			for _, candidate := range a.opts.Backends.Kinds() {
				b, err := a.opts.Backends.Get(candidate)
				if _, capable := b.(backend.MergeWorkerRunner); err == nil && capable {
					kind = candidate
					break
				}
			}
		}
	}
	b, err := a.opts.Backends.Get(kind)
	if err != nil {
		a.reportFailure(ctx, task, fmt.Sprintf("this host has no %s backend to run the merge worker with: %v", kind, err), store.FaultConfig)
		return
	}
	runner, ok := b.(backend.MergeWorkerRunner)
	if !ok {
		a.reportFailure(ctx, task, fmt.Sprintf("this host's %s backend cannot run the merge worker image", b.Kind()), store.FaultConfig)
		return
	}

	runCtx, cancel := context.WithTimeout(ctx, mergeWorkerTimeout)
	defer cancel()
	raw, err := runner.RunMergeWorker(runCtx, task.MergeImage, workspace)
	if err != nil {
		a.reportFailure(ctx, task, fmt.Sprintf("the merge worker did not answer: %v", err), store.FaultBackend)
		return
	}
	answer, err := decodeMergeAnswer(raw)
	if err != nil {
		a.reportFailure(ctx, task, fmt.Sprintf("the merge worker's answer could not be read: %v", err), store.FaultBackend)
		return
	}
	if err := a.tr.ReportMergeAnswer(ctx, task.MergeID, answer); err != nil {
		if alreadyAnswered(err) {
			a.log.Info("a merge answer was already recorded; this delivery is a lost reply", "merge", task.MergeID)
			a.report(ctx, TaskResult{TaskID: task.ID, Kind: task.Kind, OK: true, CompletedAt: a.now()})
			return
		}
		a.reportFailure(ctx, task, fmt.Sprintf("the controller did not accept the merge answer: %v", err), store.FaultBackend)
		return
	}
	if answer.Result == nil {
		a.reportFailure(ctx, task, fmt.Sprintf("the merge worker answered %s: %s", answer.Code, answer.Error), store.FaultBackend)
		return
	}
	a.report(ctx, TaskResult{TaskID: task.ID, Kind: task.Kind, OK: true, CompletedAt: a.now()})
}

// decodeMergeAnswer reads exactly the one object the worker promises. Unknown
// fields are refused rather than ignored, because this answer decides what the
// next stage reads.
func decodeMergeAnswer(raw []byte) (gitmerge.WorkerResponse, error) {
	var answer gitmerge.WorkerResponse
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&answer); err != nil {
		return gitmerge.WorkerResponse{}, err
	}
	if err := dec.Decode(new(struct{})); !errors.Is(err, io.EOF) {
		return gitmerge.WorkerResponse{}, errors.New("more than one answer on the worker's output")
	}
	if answer.Result == nil && answer.Code == "" {
		return gitmerge.WorkerResponse{}, errors.New("neither a result nor an error code")
	}
	if answer.Result != nil && answer.Code != "" {
		return gitmerge.WorkerResponse{}, errors.New("both a result and an error code")
	}
	return answer, nil
}

// makeMergeWorkspaceWritable lets the worker's own user write where it must:
// the directories, so Git can add the objects a merge produces and the worker
// can create its answer file. The files keep their modes, so a loose object
// stays read-only, which is what Git requires of it.
func makeMergeWorkspaceWritable(root string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.Chmod(path, 0o777)
		}
		return nil
	})
}

// alreadyAnswered reports the controller's refusal of a second answer, which is
// what a lost reply looks like on the next delivery. The work was recorded; the
// agent should not report the task as failed.
func alreadyAnswered(err error) bool {
	var httpErr *HTTPError
	return errors.As(err, &httpErr) && httpErr.Status == http.StatusConflict
}
