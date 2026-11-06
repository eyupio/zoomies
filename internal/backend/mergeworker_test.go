package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestRunMergeWorkerRunsTheWorkerWithOnlyTheWorkspace(t *testing.T) {
	var created ContainerCreateRequest
	var name string
	f := newFakeEngine(t, map[string]http.HandlerFunc{
		"GET " + v + "/images/{ref...}": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{
				"Id": "sha256:cached", "RepoDigests": []string{"ghcr.io/eyupio/zoomies-git@sha256:abc"},
			})
		},
		"POST " + v + "/containers/create": func(w http.ResponseWriter, r *http.Request) {
			name = r.URL.Query().Get("name")
			_ = json.NewDecoder(r.Body).Decode(&created)
			writeJSON(w, http.StatusCreated, map[string]string{"Id": "merge1"})
		},
		"POST " + v + "/containers/merge1/start": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		},
		"POST " + v + "/containers/merge1/wait": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, map[string]int{"StatusCode": 0})
		},
		"DELETE " + v + "/containers/{id}": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusNotFound, map[string]string{"message": "no such container"})
		},
	})
	b := dockerBackendFor(t, f, DockerOptions{})
	workspace := t.TempDir()
	answer := `{"result":{"tree":"tree-1"}}`
	if err := os.WriteFile(filepath.Join(workspace, "answer.json"), []byte(answer), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := b.RunMergeWorker(context.Background(), "ghcr.io/eyupio/zoomies-git:v1", workspace)
	if err != nil {
		t.Fatalf("RunMergeWorker: %v", err)
	}
	if string(got) != answer {
		t.Fatalf("answer = %q, want %q", got, answer)
	}
	if !strings.HasPrefix(name, "zoomies-merge-") {
		t.Errorf("container name = %q", name)
	}
	if created.Image != "ghcr.io/eyupio/zoomies-git@sha256:abc" {
		t.Errorf("image = %q, want the digest reference", created.Image)
	}
	if len(created.Cmd) != 5 || created.Cmd[0] != "git-worker" ||
		created.Cmd[2] != "/work/request.json" || created.Cmd[4] != "/work/answer.json" {
		t.Errorf("cmd = %v", created.Cmd)
	}
	if created.HostConfig == nil {
		t.Fatal("no host config")
	}
	if created.HostConfig.NetworkMode != "none" {
		t.Errorf("network = %q, want none", created.HostConfig.NetworkMode)
	}
	if !slices.Contains(created.HostConfig.Binds, workspace+":/work") {
		t.Errorf("binds = %v, want only the workspace", created.HostConfig.Binds)
	}
	if _, ok := created.Labels[LabelManaged]; ok {
		t.Errorf("labels = %v, want no managed label so reconciliation ignores it", created.Labels)
	}
}

func TestRunMergeWorkerReturnsWhyItWroteNoAnswer(t *testing.T) {
	f := newFakeEngine(t, map[string]http.HandlerFunc{
		"GET " + v + "/images/{ref...}": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{
				"Id": "sha256:cached", "RepoDigests": []string{"ghcr.io/eyupio/zoomies-git@sha256:abc"},
			})
		},
		"POST " + v + "/containers/create": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusCreated, map[string]string{"Id": "merge1"})
		},
		"POST " + v + "/containers/merge1/start": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		},
		"POST " + v + "/containers/merge1/wait": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, map[string]int{"StatusCode": 1})
		},
		"GET " + v + "/containers/merge1/json": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{"Id": "merge1", "Config": map[string]any{"Tty": false}})
		},
		"GET " + v + "/containers/merge1/logs": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/vnd.docker.multiplexed-stream")
			_, _ = w.Write(frame(2, "git merge-tree: boom\n"))
		},
		"DELETE " + v + "/containers/{id}": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusNotFound, map[string]string{"message": "no such container"})
		},
	})
	b := dockerBackendFor(t, f, DockerOptions{})

	_, err := b.RunMergeWorker(context.Background(), "ghcr.io/eyupio/zoomies-git:v1", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "exited with status 1") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error = %v, want the exit status and the worker's last line", err)
	}
}

func TestRunMergeWorkerRefusesARelativeWorkspace(t *testing.T) {
	f := newFakeEngine(t, map[string]http.HandlerFunc{})
	b := dockerBackendFor(t, f, DockerOptions{})
	if _, err := b.RunMergeWorker(context.Background(), "image", "relative"); err == nil {
		t.Fatal("a relative workspace was accepted")
	}
}

func mergeWorkerHandlers(over map[string]http.HandlerFunc) map[string]http.HandlerFunc {
	base := map[string]http.HandlerFunc{
		"GET " + v + "/images/{ref...}": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{"Id": "sha256:cached", "RepoDigests": []string{"ghcr.io/eyupio/zoomies-git@sha256:abc"}})
		},
		"POST " + v + "/containers/create": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusCreated, map[string]string{"Id": "merge1"})
		},
		"POST " + v + "/containers/merge1/start": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		},
		"POST " + v + "/containers/merge1/wait": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, map[string]int{"StatusCode": 0})
		},
		"DELETE " + v + "/containers/{id}": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusNotFound, map[string]string{"message": "no such container"})
		},
	}
	for k, h := range over {
		if h == nil {
			delete(base, k)
		} else {
			base[k] = h
		}
	}
	return base
}

func TestRunMergeWorkerRefusesAnEmptyImageOrAnUnusableWorkspace(t *testing.T) {
	b := dockerBackendFor(t, newFakeEngine(t, mergeWorkerHandlers(nil)), DockerOptions{})
	if _, err := b.RunMergeWorker(context.Background(), "", t.TempDir()); err == nil || !strings.Contains(err.Error(), "no merge worker image") {
		t.Fatalf("empty image error = %v", err)
	}
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := b.RunMergeWorker(context.Background(), "image", file); err == nil || !strings.Contains(err.Error(), "not a readable directory") {
		t.Fatalf("file workspace error = %v", err)
	}
}

func TestRunMergeWorkerReportsTheFirstContainerFailure(t *testing.T) {
	fail := func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "no"})
	}
	cases := map[string]map[string]http.HandlerFunc{
		"image":   {"GET " + v + "/images/{ref...}": fail},
		"create":  {"POST " + v + "/containers/create": fail},
		"start":   {"POST " + v + "/containers/merge1/start": fail},
		"wait":    {"POST " + v + "/containers/merge1/wait": fail},
		"no logs": {"GET " + v + "/containers/merge1/logs": fail},
	}
	for name, over := range cases {
		t.Run(name, func(t *testing.T) {
			// The no-logs case reaches the exit-status branch with no tail.
			if name == "no logs" {
				over["POST "+v+"/containers/merge1/wait"] = func(w http.ResponseWriter, _ *http.Request) {
					writeJSON(w, http.StatusOK, map[string]int{"StatusCode": 1})
				}
			}
			b := dockerBackendFor(t, newFakeEngine(t, mergeWorkerHandlers(over)), DockerOptions{})
			_, err := b.RunMergeWorker(context.Background(), "image", t.TempDir())
			if err == nil {
				t.Fatal("a failure was reported as success")
			}
			if name == "no logs" && !strings.HasSuffix(err.Error(), "exited with status 1") {
				t.Fatalf("an unreadable log added a tail: %v", err)
			}
		})
	}
}

func TestReadMergeAnswerRefusesAnOversizedAnswer(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, mergeWorkerAnswer), bytes.Repeat([]byte("x"), maxMergeWorkerAnswer+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readMergeAnswer(dir); err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Fatalf("oversized answer error = %v", err)
	}
}
