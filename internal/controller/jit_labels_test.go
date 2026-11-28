package controller

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/store"
)

// An ephemeral runner registers through a JIT configuration, which carries
// only the labels it is minted with. A pool that lists just its own label must
// still produce a runner GitHub offers a "runs-on: [self-hosted, linux, x64,
// ci]" job to, because the scheduler starts runners for that job on the
// understanding that every runner advertises those three.
func TestAJITRunnerAdvertisesTheImplicitLabels(t *testing.T) {
	for _, tc := range []struct {
		name      string
		noDefault bool
		jobLabels []string
		want      []string
	}{
		{"defaults", false, []string{"self-hosted", "linux", "x64", "ci"}, []string{"ci", "linux", "self-hosted", "x64"}},
		{"no default labels", true, []string{"ci"}, []string{"ci"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			inst := h.installation()
			pool := &store.Pool{
				Name:            "ci",
				InstallationID:  inst.ID,
				Labels:          []string{"ci"},
				Backend:         store.BackendDocker,
				Image:           "ghcr.io/eyupio/zoomies-runner:test",
				MaxRunners:      2,
				IdleTimeout:     store.Duration(5 * time.Minute),
				Ephemeral:       true,
				DockerMode:      store.DockerNone,
				Enabled:         true,
				NoDefaultLabels: tc.noDefault,
				Platform:        store.Platform{Arch: "amd64"},
			}
			if err := h.st.CreatePool(h.ctx, pool); err != nil {
				t.Fatalf("CreatePool: %v", err)
			}
			h.host("vm-1")
			h.deliverJob(jobEvent{Action: "queued", JobID: 1, Labels: tc.jobLabels})

			if err := h.c.Reconcile(h.ctx); err != nil {
				t.Fatalf("Reconcile: %v", err)
			}
			h.c.lifecycleCalls.Wait()

			runners := h.gh.Runners()
			if len(runners) != 1 {
				t.Fatalf("registered %d runners with GitHub, want 1", len(runners))
			}
			if got := store.NormalizeLabels(runners[0].Labels); !slices.Equal(got, tc.want) {
				t.Fatalf("runner registered with labels %v, want %v", got, tc.want)
			}
		})
	}
}

// jitHost seeds a host the way an agent describes itself: its own platform,
// the backends it can run and, for a container runtime, the platform that
// runtime runs containers on (empty from an agent too old to say).
func jitHost(h *harness, name, os, arch string, backends store.HostBackends, labels store.StringMap) *store.Host {
	h.t.Helper()
	kinds := store.StringSlice{}
	for _, b := range backends {
		kinds = append(kinds, string(b.Kind))
	}
	host := &store.Host{
		Name:          name,
		Capacity:      4,
		Backends:      kinds,
		BackendInfo:   backends,
		Labels:        labels,
		OS:            os,
		Arch:          arch,
		CPUs:          16,
		MemoryMB:      64 * 1024,
		DiskTotalMB:   500 * 1024,
		DiskFreeMB:    400 * 1024,
		LastHeartbeat: time.Now(),
	}
	if err := h.st.CreateHost(h.ctx, host); err != nil {
		h.t.Fatalf("CreateHost: %v", err)
	}
	return host
}

// registeredLabels runs one scheduling pass for a job asking for jobLabels and
// returns the labels the one runner it started was registered with.
func registeredLabels(t *testing.T, h *harness, jobLabels []string) []string {
	t.Helper()
	h.deliverJob(jobEvent{Action: "queued", JobID: 1, Labels: jobLabels})
	if err := h.c.Reconcile(h.ctx); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	h.c.lifecycleCalls.Wait()
	runners := h.gh.Runners()
	if len(runners) != 1 {
		t.Fatalf("registered %d runners with GitHub, want 1", len(runners))
	}
	return store.NormalizeLabels(runners[0].Labels)
}

// A pool that leaves its platform open is labelled by where its runner
// really runs. Before, an amd64 host took an x64 job for such a pool and the
// runner registered without x64, so GitHub never offered it the job.
func TestAJITRunnerIsLabelledByThePlatformItRunsOn(t *testing.T) {
	for _, tc := range []struct {
		name      string
		backend   store.BackendKind
		hostOS    string
		hostArch  string
		runtime   store.HostBackend
		selector  store.StringMap
		hostTags  store.StringMap
		jobLabels []string
		want      []string
	}{
		{
			name:    "container runner on an amd64 host, pool architecture unset",
			backend: store.BackendDocker, hostOS: "linux", hostArch: "amd64",
			runtime:   store.HostBackend{Kind: store.BackendDocker, Available: true, OS: "linux", Arch: "amd64"},
			jobLabels: []string{"self-hosted", "linux", "x64", "ci"},
			want:      []string{"ci", "linux", "self-hosted", "x64"},
		},
		{
			name:    "container runner on an agent that does not report its runtime's platform",
			backend: store.BackendDocker, hostOS: "linux", hostArch: "amd64",
			runtime:   store.HostBackend{Kind: store.BackendDocker, Available: true},
			jobLabels: []string{"self-hosted", "linux", "x64", "ci"},
			want:      []string{"ci", "linux", "self-hosted", "x64"},
		},
		{
			name:    "Linux container runner on a Mac",
			backend: store.BackendDocker, hostOS: "darwin", hostArch: "arm64",
			runtime:   store.HostBackend{Kind: store.BackendDocker, Available: true, OS: "linux", Arch: "arm64"},
			jobLabels: []string{"self-hosted", "linux", "arm64", "ci"},
			want:      []string{"arm64", "ci", "linux", "self-hosted"},
		},
		{
			name:    "Windows process runner chosen by host selector",
			backend: store.BackendProcess, hostOS: "windows", hostArch: "amd64",
			runtime:   store.HostBackend{Kind: store.BackendProcess, Available: true},
			selector:  store.StringMap{"os": "windows"},
			hostTags:  store.StringMap{"os": "windows"},
			jobLabels: []string{"self-hosted", "windows", "x64", "ci"},
			want:      []string{"ci", "self-hosted", "windows", "x64"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			inst := h.installation()
			pool := &store.Pool{
				Name:           "ci",
				InstallationID: inst.ID,
				Labels:         []string{"ci"},
				Backend:        tc.backend,
				MaxRunners:     2,
				IdleTimeout:    store.Duration(5 * time.Minute),
				Ephemeral:      true,
				DockerMode:     store.DockerNone,
				Enabled:        true,
				HostSelector:   tc.selector,
			}
			if tc.backend != store.BackendProcess {
				pool.Image = "ghcr.io/eyupio/zoomies-runner:test"
			}
			if err := h.st.CreatePool(h.ctx, pool); err != nil {
				t.Fatalf("CreatePool: %v", err)
			}
			tags := tc.hostTags
			if tags == nil {
				tags = store.StringMap{}
			}
			jitHost(h, "host-1", tc.hostOS, tc.hostArch, store.HostBackends{tc.runtime}, tags)

			if got := registeredLabels(t, h, tc.jobLabels); !slices.Equal(got, tc.want) {
				t.Fatalf("runner registered with labels %v, want %v", got, tc.want)
			}
		})
	}
}

// GitHub accepts at most 100 labels for a just-in-time runner. A pool whose
// own labels leave room for the implicit three registers; one more is refused
// by Zoomies, naming the pool and the limit, before GitHub is asked.
func TestAJITRunnerStaysWithinGitHubsLabelLimit(t *testing.T) {
	for _, tc := range []struct {
		name  string
		own   int
		total int
		fails bool
	}{
		{"exactly the limit", 97, 100, false},
		{"one over the limit", 98, 101, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			inst := h.installation()
			labels := []string{"ci"}
			for i := 1; len(labels) < tc.own; i++ {
				labels = append(labels, fmt.Sprintf("extra-%03d", i))
			}
			pool := &store.Pool{
				Name:           "crowded",
				InstallationID: inst.ID,
				Labels:         labels,
				Backend:        store.BackendDocker,
				Image:          "ghcr.io/eyupio/zoomies-runner:test",
				MaxRunners:     2,
				IdleTimeout:    store.Duration(5 * time.Minute),
				Ephemeral:      true,
				DockerMode:     store.DockerNone,
				Enabled:        true,
				Platform:       store.Platform{Arch: "amd64"},
			}
			if err := h.st.CreatePool(h.ctx, pool); err != nil {
				t.Fatalf("CreatePool: %v", err)
			}
			h.host("vm-1")
			h.deliverJob(jobEvent{Action: "queued", JobID: 1, Labels: []string{"self-hosted", "linux", "x64", "ci"}})
			if err := h.c.Reconcile(h.ctx); err != nil {
				t.Fatalf("Reconcile: %v", err)
			}
			h.c.lifecycleCalls.Wait()

			runners := h.gh.Runners()
			if !tc.fails {
				if len(runners) != 1 || len(store.NormalizeLabels(runners[0].Labels)) != tc.total {
					t.Fatalf("want one runner registered with %d labels, got %d runners", tc.total, len(runners))
				}
				return
			}
			if len(runners) != 0 {
				t.Fatalf("registered %d runners with GitHub, want none", len(runners))
			}
			rows, err := h.st.ListRunnersForPool(h.ctx, pool.ID)
			if err != nil {
				t.Fatalf("ListRunnersForPool: %v", err)
			}
			if len(rows) != 1 || rows[0].State != store.RunnerFailed {
				t.Fatalf("want one failed runner row, got %+v", rows)
			}
			for _, want := range []string{"pool " + pool.Name, "101 labels", "at most 100"} {
				if !strings.Contains(rows[0].Message, want) {
					t.Fatalf("failure %q does not say %q", rows[0].Message, want)
				}
			}
		})
	}
}

// A pool's stated platform is a promise to GitHub about the machine. Where the
// host says its runner will run as something else, the runner is refused
// before it is registered, naming the pool and both platforms, rather than
// minted with labels that would bring it jobs it cannot run. Where the host
// has said nothing (an agent that predates the report), the pool's platform is
// used as before.
func TestAJITRunnerWhosePoolContradictsItsHostIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name      string
		platform  store.Platform
		hostOS    string
		hostArch  string
		runtime   store.HostBackend
		jobLabels []string
		want      []string // registered labels, when the runner is not refused
		refusal   []string // what the failure must say, when it is
	}{
		{
			name:     "macos pool on a Mac whose Docker runs Linux containers",
			platform: store.Platform{OS: "macos", Arch: "arm64"}, hostOS: "darwin", hostArch: "arm64",
			runtime:   store.HostBackend{Kind: store.BackendDocker, Available: true, OS: "linux", Arch: "arm64"},
			jobLabels: []string{"self-hosted", "macos", "arm64", "ci"},
			refusal:   []string{"declares macos/arm64", "would run as linux/arm64"},
		},
		{
			name:     "macos process pool on a Linux host with an unknown distro",
			platform: store.Platform{OS: "macos", Arch: "amd64"}, hostOS: "linux", hostArch: "amd64",
			runtime:   store.HostBackend{Kind: store.BackendProcess, Available: true},
			jobLabels: []string{"self-hosted", "ci"},
			refusal:   []string{"declares macos/amd64", "would run as linux/amd64"},
		},
		{
			name:     "amd64 pool on a runtime that runs arm64",
			platform: store.Platform{Arch: "amd64"}, hostOS: "linux", hostArch: "amd64",
			runtime:   store.HostBackend{Kind: store.BackendDocker, Available: true, OS: "linux", Arch: "arm64"},
			jobLabels: []string{"self-hosted", "linux", "x64", "ci"},
			refusal:   []string{"declares amd64", "would run as linux/arm64"},
		},
		{
			name:     "macos pool on an agent that does not report its runtime's platform",
			platform: store.Platform{OS: "macos", Arch: "arm64"}, hostOS: "darwin", hostArch: "arm64",
			runtime:   store.HostBackend{Kind: store.BackendDocker, Available: true},
			jobLabels: []string{"self-hosted", "macos", "arm64", "ci"},
			want:      []string{"arm64", "ci", "macos", "self-hosted"},
		},
		{
			name:     "a stated platform the runtime agrees with",
			platform: store.Platform{OS: "ubuntu", Arch: "amd64"}, hostOS: "linux", hostArch: "amd64",
			runtime:   store.HostBackend{Kind: store.BackendDocker, Available: true, OS: "linux", Arch: "amd64"},
			jobLabels: []string{"self-hosted", "linux", "x64", "ci"},
			want:      []string{"ci", "linux", "self-hosted", "x64"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			inst := h.installation()
			pool := &store.Pool{
				Name:           "ci",
				InstallationID: inst.ID,
				Labels:         []string{"ci"},
				Backend:        tc.runtime.Kind,
				MaxRunners:     2,
				IdleTimeout:    store.Duration(5 * time.Minute),
				Ephemeral:      true,
				DockerMode:     store.DockerNone,
				Enabled:        true,
				Platform:       tc.platform,
			}
			if tc.runtime.Kind != store.BackendProcess {
				pool.Image = "ghcr.io/example/custom-runner:test"
			}
			if err := h.st.CreatePool(h.ctx, pool); err != nil {
				t.Fatalf("CreatePool: %v", err)
			}
			jitHost(h, "host-1", tc.hostOS, tc.hostArch, store.HostBackends{tc.runtime}, store.StringMap{})
			jobLabels := tc.jobLabels
			if tc.refusal != nil {
				jobLabels = []string{"self-hosted", "ci"}
			}
			h.deliverJob(jobEvent{Action: "queued", JobID: 1, Labels: jobLabels})
			if err := h.c.Reconcile(h.ctx); err != nil {
				t.Fatalf("Reconcile: %v", err)
			}
			h.c.lifecycleCalls.Wait()

			runners := h.gh.Runners()
			if tc.refusal == nil {
				if len(runners) != 1 {
					t.Fatalf("registered %d runners with GitHub, want 1", len(runners))
				}
				if got := store.NormalizeLabels(runners[0].Labels); !slices.Equal(got, tc.want) {
					t.Fatalf("runner registered with labels %v, want %v", got, tc.want)
				}
				return
			}
			if len(runners) != 0 {
				t.Fatalf("registered %d runners with GitHub (labels %v), want none", len(runners), runners[0].Labels)
			}
			rows, err := h.st.ListRunnersForPool(h.ctx, pool.ID)
			if err != nil {
				t.Fatalf("ListRunnersForPool: %v", err)
			}
			if len(rows) != 1 || rows[0].State != store.RunnerFailed {
				t.Fatalf("want one failed runner row, got %+v", rows)
			}
			for _, want := range append([]string{"pool " + pool.Name, "host host-1"}, tc.refusal...) {
				if !strings.Contains(rows[0].Message, want) {
					t.Fatalf("failure %q does not say %q", rows[0].Message, want)
				}
			}
			if strings.Contains(rows[0].Message, "GitHub would not register") {
				t.Fatalf("failure %q blames GitHub for a check Zoomies made", rows[0].Message)
			}
		})
	}
}
