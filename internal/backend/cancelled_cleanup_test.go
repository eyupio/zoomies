package backend

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/eyupio/zoomies/internal/store"
)

func TestCancelledContainerStartStillRemovesContainerAndScratch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	spec := jitSpec()
	spec.WorkDir = filepath.Join(t.TempDir(), "scratch")
	var created, removed atomic.Bool
	f := newFakeEngine(t, map[string]http.HandlerFunc{
		"GET " + v + "/images/{ref...}": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, 200, map[string]any{"Id": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"})
		},
		"POST " + v + "/containers/create": func(w http.ResponseWriter, r *http.Request) {
			created.Store(true)
			writeJSON(w, 201, map[string]string{"Id": "c1"})
		},
		"POST " + v + "/containers/c1/start": func(w http.ResponseWriter, r *http.Request) {
			cancel()
			w.WriteHeader(500)
		},
		"GET " + v + "/containers/{id}/json": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, 200, ContainerInspect{Config: &ContainerConfig{Labels: map[string]string{LabelName: spec.Name, LabelWorkDir: spec.WorkDir}}})
		},
		"DELETE " + v + "/containers/{id}": func(w http.ResponseWriter, r *http.Request) {
			if created.Load() && r.PathValue("id") == containerName(spec.Name) {
				removed.Store(true)
			}
			w.WriteHeader(204)
		},
	})
	b := dockerBackendFor(t, f, DockerOptions{PullPolicy: PullNever})
	if _, err := b.Create(ctx, spec); err == nil {
		t.Fatal("cancelled start succeeded")
	}
	if !removed.Load() {
		t.Fatal("expired create context prevented container removal")
	}
	if _, err := os.Stat(spec.WorkDir); !os.IsNotExist(err) {
		t.Fatalf("scratch survived failed start: %v", err)
	}
}

func TestFailedDinDPreparationCleansOnlyOwnedScratch(t *testing.T) {
	for _, preexisting := range []bool{false, true} {
		t.Run(map[bool]string{false: "owned", true: "operator directory"}[preexisting], func(t *testing.T) {
			spec := jitSpec()
			spec.DockerMode = store.DockerDinD
			spec.WorkDir = filepath.Join(t.TempDir(), "scratch")
			if preexisting {
				if err := os.Mkdir(spec.WorkDir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			f := newFakeEngine(t, map[string]http.HandlerFunc{
				"GET " + v + "/images/{ref...}": func(w http.ResponseWriter, r *http.Request) {
					if r.PathValue("ref") == DefaultDinDImage+"/json" {
						w.WriteHeader(404)
						return
					}
					writeJSON(w, 200, map[string]string{"Id": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"})
				},
				"DELETE " + v + "/containers/{id}": func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) },
			})
			b := dockerBackendFor(t, f, DockerOptions{PullPolicy: PullNever})
			if _, err := b.Create(context.Background(), spec); err == nil {
				t.Fatal("missing DinD image was accepted")
			}
			_, err := os.Stat(spec.WorkDir)
			if preexisting && err != nil {
				t.Fatalf("operator directory was removed: %v", err)
			}
			if !preexisting && !os.IsNotExist(err) {
				t.Fatalf("allocated scratch leaked: %v", err)
			}
		})
	}
}

func TestBuildCachePruningUsesBudgetWithoutPruningOtherResources(t *testing.T) {
	var calls int
	f := newFakeEngine(t, map[string]http.HandlerFunc{
		"POST " + v + "/build/prune": func(w http.ResponseWriter, r *http.Request) {
			calls++
			if r.URL.Query().Get("keep-storage") != "5368709120" || r.URL.Query().Get("all") != "true" {
				t.Errorf("wrong cache prune parameters: %s", r.URL.RawQuery)
			}
			writeJSON(w, 200, map[string]int64{"SpaceReclaimed": 1234})
		},
	})
	b := dockerBackendFor(t, f, DockerOptions{})
	freed, err := b.PruneBuildCache(context.Background(), 5<<30)
	if err != nil || freed != 1234 {
		t.Fatalf("prune = %d, %v", freed, err)
	}
	_, _ = b.PruneBuildCache(context.Background(), 0)
	b.fl.kind = store.BackendPodman
	_, _ = b.PruneBuildCache(context.Background(), 5<<30)
	if calls != 1 {
		t.Fatalf("disabled/Podman pruning made %d calls", calls)
	}
	if len(f.seen) != 1 {
		t.Fatalf("cleanup accessed other resources: %d calls", len(f.seen))
	}
}

func TestRunnerAndSidecarLogsAreBounded(t *testing.T) {
	for _, cfg := range []ContainerCreateRequest{buildRunnerConfig(jitSpec(), dockerFlavor(), containerOptions{}), buildDinDConfig(jitSpec(), dockerFlavor(), containerOptions{})} {
		logs := cfg.HostConfig.LogConfig
		if logs == nil || logs.Type != "json-file" || logs.Config["max-size"] != "10m" || logs.Config["max-file"] != "3" {
			t.Fatalf("unbounded container logs: %+v", logs)
		}
	}
}
