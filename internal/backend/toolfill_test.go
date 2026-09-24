package backend

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/store"
)

func toolFillSpec() Spec {
	return Spec{
		PoolID:     "pool_1",
		PoolName:   "linux-x64",
		Image:      "ghcr.io/eyupio/zoomies-runner:v1",
		PullPolicy: store.PullIfNotPresent,
		Cache:      store.CacheConfig{Enabled: true, Tools: true, Scope: store.CacheScopePool},
		Env:        map[string]string{"HTTPS_PROXY": "http://proxy.pool:3128", "SECRET_TOKEN": "not-for-the-fill"},
		RunAsRoot:  true,
	}
}

// The fill writes what the runners read, so it runs as the image's runner
// user even for a pool whose jobs run as root -- a cache of root's files is
// one a later job cannot update. It sees the tool cache and nothing else of
// the host's, and carries no managed label, so the agent's reconciliation
// never mistakes it for a runner and reaps it mid-download.
func TestTheFillContainerIsTheRunnerUserWithOnlyTheToolCache(t *testing.T) {
	tools := []ToolRequest{{Tool: "python", Version: "3.12"}, {Tool: "java", Version: "21", Distribution: "temurin"}}
	cfg := buildToolFillConfig(toolFillSpec(), dockerFlavor(), "/var/lib/zoomies/shared/cache/tools/pool-1", "", nil, tools)

	if cfg.User != "runner" {
		t.Errorf("user = %q, want the runner user", cfg.User)
	}
	if _, managed := cfg.Labels[LabelManaged]; managed {
		t.Error("the fill carries the managed label, so reconciliation would treat it as a runner")
	}
	if !slices.Equal(cfg.HostConfig.Binds, []string{"/var/lib/zoomies/shared/cache/tools/pool-1:" + RunnerToolCacheMount}) {
		t.Errorf("binds = %v, want only the tool cache", cfg.HostConfig.Binds)
	}
	env := envMap(cfg.Env)
	if env[EnvToolsDirectory] != RunnerToolCacheMount {
		t.Errorf("%s = %q", EnvToolsDirectory, env[EnvToolsDirectory])
	}
	if env["ZOOMIES_TOOL_REQUESTS"] != "python 3.12 \njava 21 temurin\n" {
		t.Errorf("requests = %q", env["ZOOMIES_TOOL_REQUESTS"])
	}
	// The pool's proxy is its way out; the rest of its env is its jobs'.
	if env["HTTPS_PROXY"] != "http://proxy.pool:3128" {
		t.Errorf("the pool's proxy was not passed on: %v", env)
	}
	if _, ok := env["SECRET_TOKEN"]; ok {
		t.Error("a pool variable that is not a proxy reached the fill")
	}
	if cfg.HostConfig.NanoCPUs == 0 || cfg.HostConfig.Memory == 0 {
		t.Error("the fill has no resource limit, so it can take a running job's share of the host")
	}
	if len(cfg.Entrypoint) < 3 || cfg.Entrypoint[0] != "/bin/bash" || cfg.Entrypoint[2] != toolFillScript {
		t.Errorf("entrypoint = %.60q, want the embedded script run by bash", cfg.Entrypoint)
	}
}

func TestTheFillTrustsTheHostsExtraCA(t *testing.T) {
	cfg := buildToolFillConfig(toolFillSpec(), dockerFlavor(), "/tools", "/etc/acme/proxy-ca.pem", nil, []ToolRequest{{Tool: "node", Version: "22"}})
	if !slices.Contains(cfg.HostConfig.Binds, "/etc/acme/proxy-ca.pem:"+ExtraCAPath+":ro") {
		t.Errorf("binds = %v, want the CA mounted read-only", cfg.HostConfig.Binds)
	}
	if envMap(cfg.Env)[EnvExtraCAFile] != ExtraCAPath {
		t.Errorf("%s is not set, so the script cannot find the CA", EnvExtraCAFile)
	}
}

func TestParseToolFillsReadsEachOutcome(t *testing.T) {
	out := strings.Join([]string{
		"Collecting pip",
		"zoomies-fill installed python 3.12 3.12.7",
		"zoomies-fill present node 22 22.11.0",
		"zoomies-fill skipped dotnet 8.0 setup-dotnet installs into its own folder, not the tool cache",
		"zoomies-fill failed go 1.27 could not download go1.27.1.linux-amd64.tar.gz",
		"zoomies-fill nonsense line",
		"curl: (6) Could not resolve host",
	}, "\n")
	fills, tail := parseToolFills(strings.NewReader(out))
	want := []ToolFill{
		{Tool: "python", Request: "3.12", Outcome: ToolInstalled, Version: "3.12.7"},
		{Tool: "node", Request: "22", Outcome: ToolPresent, Version: "22.11.0"},
		{Tool: "dotnet", Request: "8.0", Outcome: ToolSkipped, Reason: "setup-dotnet installs into its own folder, not the tool cache"},
		{Tool: "go", Request: "1.27", Outcome: ToolFailed, Reason: "could not download go1.27.1.linux-amd64.tar.gz"},
	}
	if !slices.Equal(fills, want) {
		t.Errorf("fills = %+v\nwant %+v", fills, want)
	}
	if !slices.Equal(tail, []string{"Collecting pip", "curl: (6) Could not resolve host"}) {
		t.Errorf("tail = %q", tail)
	}
}

func TestFillToolCacheRunsTheFillInThePoolsToolCache(t *testing.T) {
	shared := t.TempDir()
	var created ContainerCreateRequest
	var removed []string
	f := newFakeEngine(t, map[string]http.HandlerFunc{
		"DELETE " + v + "/containers/{id}": func(w http.ResponseWriter, r *http.Request) {
			removed = append(removed, r.PathValue("id"))
			writeJSON(w, http.StatusNotFound, map[string]string{"message": "no such container"})
		},
		"GET " + v + "/images/{ref...}": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{"Id": "sha256:cached", "RepoDigests": []string{"ghcr.io/eyupio/zoomies-runner@sha256:abc"}})
		},
		"POST " + v + "/containers/create": func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&created)
			writeJSON(w, http.StatusCreated, map[string]any{"Id": "fill1"})
		},
		"POST " + v + "/containers/fill1/start": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		},
		"POST " + v + "/containers/fill1/wait": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{"StatusCode": 0})
		},
		"GET " + v + "/containers/fill1/json": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{"Id": "fill1", "Config": map[string]any{"Tty": false}})
		},
		"GET " + v + "/containers/fill1/logs": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/vnd.docker.multiplexed-stream")
			_, _ = w.Write(frame(1, "zoomies-fill installed node 22 22.11.0\n"))
			_, _ = w.Write(frame(1, "zoomies-fill skipped java 17 only the temurin distribution is filled, and this asks for zulu\n"))
		},
	})
	b := dockerBackendFor(t, f, DockerOptions{SharedDir: shared})

	ctx, cancel := context.WithTimeout(context.Background(), 10e9)
	defer cancel()
	fills, err := b.FillToolCache(ctx, toolFillSpec(), []ToolRequest{{Tool: "node", Version: "22"}, {Tool: "java", Version: "17", Distribution: "zulu"}})
	if err != nil {
		t.Fatalf("FillToolCache: %v", err)
	}
	if len(fills) != 2 || fills[0].Version != "22.11.0" || fills[1].Outcome != ToolSkipped {
		t.Errorf("fills = %+v", fills)
	}
	// The folder a runner of this pool is bound, created before the daemon is
	// asked to bind it, or the daemon makes it root's.
	dir := filepath.Join(shared, "cache", "tools", "pool-1")
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Fatalf("the tool cache folder was not created: %v", err)
	}
	if !slices.Contains(created.HostConfig.Binds, dir+":"+RunnerToolCacheMount) {
		t.Errorf("binds = %v, want %s", created.HostConfig.Binds, dir)
	}
	// Removed before, in case a cancelled fill left one, and after.
	if len(removed) != 2 || removed[0] != "zoomies-toolfill-pool-1" {
		t.Errorf("removed = %v", removed)
	}
}

func TestFillToolCacheRefusesAPoolWithoutAToolCache(t *testing.T) {
	b := &DockerBackend{sharedDir: t.TempDir(), log: quietLogger()}
	spec := toolFillSpec()
	spec.Cache.Tools = false
	if _, err := b.FillToolCache(context.Background(), spec, []ToolRequest{{Tool: "go", Version: "1.27"}}); err == nil ||
		!strings.Contains(err.Error(), "keeps no tool cache") {
		t.Fatalf("err = %v, want one saying the pool keeps no tool cache", err)
	}
}

// Waiting on a container is the one Engine call with no natural end, so it
// has no default deadline to fall back on either: the caller must give one.
func TestContainerWaitNeedsADeadline(t *testing.T) {
	c, err := NewAPIClient("tcp://127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.ContainerWait(context.Background(), "c1"); err == nil || !strings.Contains(err.Error(), "deadline") {
		t.Fatalf("ContainerWait without a deadline = %v, want a refusal", err)
	}
}
