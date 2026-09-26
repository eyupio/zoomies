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
