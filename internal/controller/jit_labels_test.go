package controller

import (
	"slices"
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
