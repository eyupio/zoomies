package scheduler

import (
	"github.com/eyupio/zoomies/internal/store"
	"testing"
	"time"
)

func TestReadinessPlacementRespectsFitAndStartupBacklog(t *testing.T) {
	for _, tc := range []struct {
		name  string
		busy  int
		small bool
		want  string
	}{
		{"faster history", 0, false, "a"},
		{"startup queue reverses preference", 3, false, "b"},
		{"history never overrides memory fit", 0, true, "b"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := limited("build", 2, 2048)
			a, b := sized("a", 8, 16, 32768, 100000), sized("b", 8, 16, 32768, 100000)
			if tc.small {
				a = sized("a", 8, 16, 1024, 100000)
			}
			rs := map[string][]*store.Runner{}
			for range tc.busy {
				rs[p.ID] = append(rs[p.ID], &store.Runner{HostID: "a", PoolID: p.ID, State: store.RunnerRegistering})
			}
			hs := newHostSet([]*store.Host{a, b}, []*store.Pool{p}, rs, now)
			hs.preferReadiness = true
			hs.readiness = map[string]map[string]time.Duration{p.ID: {"a": 10 * time.Second, "b": 25 * time.Second}}
			got := hs.place(p, 1)
			if len(got) != 1 || got[0] != tc.want {
				t.Fatalf("placement=%v want %s", got, tc.want)
			}
		})
	}
}

func TestReadinessWithoutEvidenceFallsBackToHeadroom(t *testing.T) {
	p := limited("build", 2, 2048)
	a, b := sized("a", 4, 4, 8192, 100000), sized("b", 4, 16, 32768, 100000)
	hs := newHostSet([]*store.Host{a, b}, []*store.Pool{p}, nil, now)
	hs.preferReadiness = true
	if got := hs.place(p, 1); len(got) != 1 || got[0] != "b" {
		t.Fatalf("placement=%v", got)
	}
}

// The agent starts DinD creates and everything else on separate lanes, so a
// warming daemon is not ahead of a daemonless runner. Charging every pool for
// the whole host's warming sent a daemonless pool to a slower host for no
// reason; this holds the estimate to the lane the pool will actually wait in.
func TestReadinessCountsWarmingRunnersPerStartupLane(t *testing.T) {
	plain := limited("plain", 2, 2048)
	dind := limited("dind", 2, 2048)
	dind.DockerMode = store.DockerDinD
	a, b := sized("a", 8, 16, 32768, 100000), sized("b", 8, 16, 32768, 100000)
	// One DinD runner is warming on a. It is ahead of DinD creates only, and
	// its reservation is what makes a the worse host on headroom alone.
	runners := map[string][]*store.Runner{
		dind.ID: {{HostID: "a", PoolID: dind.ID, State: store.RunnerRegistering}},
	}
	for _, tc := range []struct {
		name    string
		pool    *store.Pool
		history map[string]time.Duration
		want    string
	}{
		{"a daemonless pool is not charged for the DinD lane", plain, map[string]time.Duration{"a": 10 * time.Second, "b": 25 * time.Second}, "a"},
		{"a DinD pool is", dind, map[string]time.Duration{"a": 10 * time.Second, "b": 10 * time.Second}, "b"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hs := newHostSet([]*store.Host{a, b}, []*store.Pool{plain, dind}, runners, now)
			hs.preferReadiness = true
			hs.readiness = map[string]map[string]time.Duration{tc.pool.ID: tc.history}
			got := hs.place(tc.pool, 1)
			if len(got) != 1 || got[0] != tc.want {
				t.Fatalf("placement=%v want %s", got, tc.want)
			}
		})
	}
}
