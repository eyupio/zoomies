package backend

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/store"
)

// A throttle scales a container's quota from what it was created with, and an
// agent that adopted the container after a restart has no memory of that. The
// container itself has to say, and both halves of a docker-in-docker pair
// have to say it, because each is throttled on its own.
func TestBothContainersOfARunnerCarryTheirLimitsAndWhereTheyCameFrom(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name      string
		resources store.Resources
		source    string
		wantCPUs  string
		wantMem   string
		wantFrom  string
	}{
		{"the pool's own limits", store.Resources{CPUs: 2.5, MemoryMB: 4096}, store.AllocationFromPool, "2.5", "4096", "pool"},
		{"the host's default share", store.Resources{CPUs: 0.75, MemoryMB: 1900}, store.AllocationFromHost, "0.75", "1900", "host"},
		// A controller that predates the source field still sets limits, and
		// the only limits it knows are the pool's.
		{"a limit with no source named", store.Resources{CPUs: 1}, "", "1", "", "pool"},
		{"a memory limit alone", store.Resources{MemoryMB: 512}, store.AllocationFromHost, "", "512", "host"},
		{"no limits at all", store.Resources{}, store.AllocationFromHost, "", "", ""},
	}
	for _, c := range cases {
		spec := jitSpec()
		spec.Resources = c.resources
		spec.ResourcesSource = c.source
		spec.DockerMode = store.DockerDinD
		for _, cfg := range []struct {
			what   string
			labels map[string]string
		}{
			{"runner", buildRunnerConfig(spec, dockerFlavor(), containerOptions{Now: now}).Labels},
			{"sidecar", buildDinDConfig(spec, dockerFlavor(), containerOptions{Now: now, DinDImage: DefaultDinDImage}).Labels},
		} {
			got := [3]string{cfg.labels[LabelCPUs], cfg.labels[LabelMemoryMB], cfg.labels[LabelLimitsFrom]}
			want := [3]string{c.wantCPUs, c.wantMem, c.wantFrom}
			if got != want {
				t.Errorf("%s, %s: labels cpus/memory/from = %q, want %q", c.name, cfg.what, got, want)
			}
		}
	}
}

// The listing is where an adopted runner's base allocation comes from, so a
// label that is missing or mangled must read as "no limit" rather than break
// the listing: the worst outcome of a bad label is a runner the throttle
// leaves alone.
func TestListReadsARunnersLimitsBackFromItsLabels(t *testing.T) {
	f := newFakeEngine(t, map[string]http.HandlerFunc{
		"GET " + v + "/containers/json": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, 200, []ContainerSummary{
				{ID: "c1", State: "running", Labels: map[string]string{
					LabelRole: roleRunner, LabelName: "runner-1", LabelRunnerID: "run_1",
					LabelCPUs: "1.5", LabelMemoryMB: "2048", LabelLimitsFrom: "host",
				}},
				{ID: "c2", State: "running", Labels: map[string]string{
					LabelRole: roleRunner, LabelName: "runner-2", LabelRunnerID: "run_2",
					LabelCPUs: "lots", LabelMemoryMB: "-3",
				}},
				{ID: "c3", State: "running", Labels: map[string]string{
					LabelRole: roleRunner, LabelName: "runner-3", LabelRunnerID: "run_3",
				}},
			})
		},
	})
	b := dockerBackendFor(t, f, DockerOptions{})

	got, err := b.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	byID := map[Handle]store.Resources{}
	for _, w := range got {
		byID[w.Handle] = w.Resources
	}
	if r := byID["c1"]; r.CPUs != 1.5 || r.MemoryMB != 2048 {
		t.Fatalf("c1 resources = %+v, want 1.5 CPUs and 2048 MB from its labels", r)
	}
	if r := byID["c2"]; r != (store.Resources{}) {
		t.Fatalf("c2 resources = %+v, want nothing from labels that do not parse", r)
	}
	if r := byID["c3"]; r != (store.Resources{}) {
		t.Fatalf("c3 resources = %+v, want nothing from a container with no labels", r)
	}
}

// updateEngine is a fake daemon holding one runner and, optionally, its
// sidecar, that records every quota update it is sent.
type updateEngine struct {
	*fakeEngine
	mu      sync.Mutex
	updates map[string][]map[string]any
}

func newUpdateEngine(t *testing.T, runnerNanos, sidecarNanos int64, withSidecar bool) *updateEngine {
	t.Helper()
	u := &updateEngine{updates: map[string][]map[string]any{}}
	inspect := func(id string, nanos int64, labels map[string]string) *ContainerInspect {
		return &ContainerInspect{
			ID:         id,
			State:      &ContainerState{Status: "running", Running: true},
			Config:     &ContainerConfig{Labels: labels},
			HostConfig: &HostConfig{NanoCPUs: nanos},
		}
	}
	runnerLabels := map[string]string{LabelRole: roleRunner, LabelName: "runner-1", LabelRunnerID: "run_1"}
	sidecarLabels := map[string]string{LabelRole: roleDinD, LabelName: "runner-1-dind", LabelDinDFor: "runner-1", LabelRunnerID: "run_1"}
	u.fakeEngine = newFakeEngine(t, map[string]http.HandlerFunc{
		"GET " + v + "/containers/{id}/json": func(w http.ResponseWriter, r *http.Request) {
			switch r.PathValue("id") {
			case "c1":
				writeJSON(w, 200, inspect("c1", runnerNanos, runnerLabels))
			case "d1":
				if withSidecar {
					writeJSON(w, 200, inspect("d1", sidecarNanos, sidecarLabels))
					return
				}
				fallthrough
			default:
				writeJSON(w, http.StatusNotFound, map[string]string{"message": "no such container"})
			}
		},
		"GET " + v + "/containers/json": func(w http.ResponseWriter, r *http.Request) {
			// The sidecar is found by the runner's name, and by nothing looser:
			// a filter on the managed label alone would throttle every sidecar
			// on the host for one runner's sake.
			if !strings.Contains(r.Form.Get("filters"), LabelDinDFor+"=runner-1") {
				t.Errorf("sidecar listing did not filter on the runner's name: %s", r.Form.Get("filters"))
			}
			if !withSidecar {
				writeJSON(w, 200, []ContainerSummary{})
				return
			}
			writeJSON(w, 200, []ContainerSummary{{ID: "d1", State: "running", Labels: sidecarLabels}})
		},
		"POST " + v + "/containers/{id}/update": func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			u.mu.Lock()
			u.updates[r.PathValue("id")] = append(u.updates[r.PathValue("id")], body)
			u.mu.Unlock()
			writeJSON(w, http.StatusOK, map[string]any{"Warnings": []string{}})
		},
	})
	return u
}

func (u *updateEngine) sent(id string) []map[string]any {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.updates[id]
}

// The agent sends the same figure on every beat and keeps no durable record
// of what it last sent, so a container whose quota already matches must cost
// no request; and when it does not match, the request must carry the quota
// and nothing else, because a zero memory field in that body would be read by
// the daemon as "remove the memory limit".
func TestUpdateResourcesMovesTheCPUQuotaOnlyWhenItDiffersAndSendsNothingElse(t *testing.T) {
	cases := []struct {
		name        string
		current     int64
		cpus        float64
		wantUpdates int
		wantNanos   float64
	}{
		{"already at the quota", 1_000_000_000, 1, 0, 0},
		{"throttled to half", 2_000_000_000, 1, 1, 1_000_000_000},
		{"restored to the base", 1_000_000_000, 2, 1, 2_000_000_000},
		{"a share in hundredths", 750_000_000, 0.38, 1, 380_000_000},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			u := newUpdateEngine(t, c.current, 0, false)
			b := dockerBackendFor(t, u.fakeEngine, DockerOptions{})
			err := b.UpdateResources(context.Background(), "c1", store.Resources{CPUs: c.cpus, MemoryMB: 4096, PidsLimit: 512})
			if err != nil {
				t.Fatalf("update: %v", err)
			}
			got := u.sent("c1")
			if len(got) != c.wantUpdates {
				t.Fatalf("updates sent = %d (%v), want %d", len(got), got, c.wantUpdates)
			}
			for _, body := range got {
				if body["NanoCpus"] != c.wantNanos {
					t.Fatalf("NanoCpus = %v, want %v", body["NanoCpus"], c.wantNanos)
				}
				if _, ok := body["Memory"]; ok || len(body) != 1 {
					t.Fatalf("the update carried more than the CPU quota: %v", body)
				}
			}
		})
	}
}

// Under docker_mode dind the builds run in the sidecar, so a throttle that
// only reached the runner process would slow the wrong container. The sidecar
// gets the same quota, and the same "only when it differs" rule.
func TestUpdateResourcesThrottlesTheDinDSidecarWithItsRunner(t *testing.T) {
	u := newUpdateEngine(t, 2_000_000_000, 2_000_000_000, true)
	b := dockerBackendFor(t, u.fakeEngine, DockerOptions{})
	if err := b.UpdateResources(context.Background(), "c1", store.Resources{CPUs: 1}); err != nil {
		t.Fatalf("update: %v", err)
	}
	for _, id := range []string{"c1", "d1"} {
		got := u.sent(id)
		if len(got) != 1 || got[0]["NanoCpus"] != float64(1_000_000_000) {
			t.Fatalf("%s updates = %v, want one update to 1 CPU", id, got)
		}
	}

	// The runner already throttled but the sidecar not: only the sidecar moves.
	u = newUpdateEngine(t, 1_000_000_000, 2_000_000_000, true)
	b = dockerBackendFor(t, u.fakeEngine, DockerOptions{})
	if err := b.UpdateResources(context.Background(), "c1", store.Resources{CPUs: 1}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if got := u.sent("c1"); len(got) != 0 {
		t.Fatalf("the runner was updated although its quota already matched: %v", got)
	}
	if got := u.sent("d1"); len(got) != 1 {
		t.Fatalf("sidecar updates = %v, want exactly one", got)
	}
}

// A runner that finished between the beat and the update is not a failure to
// throttle; the agent needs to tell that apart from a daemon that refused.
func TestUpdateResourcesReportsAGoneContainerAsNotFound(t *testing.T) {
	u := newUpdateEngine(t, 0, 0, false)
	b := dockerBackendFor(t, u.fakeEngine, DockerOptions{})
	err := b.UpdateResources(context.Background(), "vanished", store.Resources{CPUs: 1})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// A runner with no CPU limit has no quota to scale, and the update endpoint
// reads a zero as "leave it alone" rather than "remove the limit", so asking
// would be a request that means nothing.
func TestUpdateResourcesAsksNothingForARunnerWithNoCPULimit(t *testing.T) {
	u := newUpdateEngine(t, 0, 0, false)
	b := dockerBackendFor(t, u.fakeEngine, DockerOptions{})
	if err := b.UpdateResources(context.Background(), "c1", store.Resources{MemoryMB: 4096}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if u.request(http.MethodPost, v+"/containers/c1/update") != nil || u.request(http.MethodGet, v+"/containers/c1/json") != nil {
		t.Fatal("the daemon was asked about a runner that has no quota to move")
	}
}

// An operator told to raise a pool field nobody set is sent to the wrong
// page. A limit that was the host's default share moves with the host's
// capacity, or with a limit of the pool's own, and the message has to say so.
func TestAnOOMKillSaysWhatToChangeForEachSourceOfTheLimit(t *testing.T) {
	cases := []struct {
		name   string
		labels map[string]string
		want   []string
		unwant []string
	}{
		{"the pool's limit", map[string]string{LabelLimitsFrom: "pool"}, []string{"raise the pool's memory_mb"}, []string{"capacity"}},
		{"no source recorded", nil, []string{"raise the pool's memory_mb"}, []string{"capacity"}},
		{"the host's default share", map[string]string{LabelLimitsFrom: "host"},
			[]string{"host's default share", "memory_mb on the pool", "lower the host's capacity"}, []string{"raise the pool's"}},
	}
	for _, c := range cases {
		st := statusFromInspect("c", &ContainerInspect{
			State:  &ContainerState{Status: "exited", ExitCode: 137, OOMKilled: true},
			Config: &ContainerConfig{Labels: c.labels},
		})
		if st.Phase != PhaseFailed {
			t.Errorf("%s: phase = %q, want failed", c.name, st.Phase)
		}
		for _, w := range c.want {
			if !strings.Contains(st.Message, w) {
				t.Errorf("%s: message %q does not say %q", c.name, st.Message, w)
			}
		}
		for _, w := range c.unwant {
			if strings.Contains(st.Message, w) {
				t.Errorf("%s: message %q should not say %q", c.name, st.Message, w)
			}
		}
	}
}
