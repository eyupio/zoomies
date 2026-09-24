package scheduler

import (
	"testing"

	"github.com/eyupio/zoomies/internal/store"
)

// A runner registered with --no-default-labels advertises only its own labels,
// so GitHub never offers it a job asking for self-hosted, linux or x64. Were
// the scheduler to match such a job anyway, it would start a runner the job can
// never reach and the job would wait while the pool reported itself busy.
func TestAPoolWithoutDefaultLabelsMatchesOnlyWhatItLists(t *testing.T) {
	bare := &store.Pool{Labels: store.StringSlice{"gpu"}, NoDefaultLabels: true}
	listed := &store.Pool{Labels: store.StringSlice{"gpu", "self-hosted", "linux"}, NoDefaultLabels: true}
	usual := &store.Pool{Labels: store.StringSlice{"gpu"}}

	tests := []struct {
		name string
		pool *store.Pool
		job  []string
		want bool
	}{
		{"its own label alone", bare, []string{"gpu"}, true},
		{"self-hosted it does not advertise", bare, []string{"self-hosted", "gpu"}, false},
		{"an os it does not advertise", bare, []string{"linux", "gpu"}, false},
		{"implicit labels it lists itself", listed, []string{"self-hosted", "linux", "gpu"}, true},
		{"an arch it still does not list", listed, []string{"x64", "gpu"}, false},
		{"an ordinary pool is unchanged", usual, []string{"self-hosted", "linux", "gpu"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := PoolMatches(tc.pool, tc.job); got != tc.want {
				t.Fatalf("PoolMatches(%v, %v) = %v, want %v", tc.pool.Labels, tc.job, got, tc.want)
			}
		})
	}
}

func TestBestPoolPassesOverAPoolWithoutDefaultLabelsForASelfHostedJob(t *testing.T) {
	// Named to sort first, so only the label rule can keep it from winning.
	bare := &store.Pool{ID: "p1", Name: "a-bare", InstallationID: "ins_one", Labels: store.StringSlice{"gpu"}, NoDefaultLabels: true, Enabled: true}
	usual := &store.Pool{ID: "p2", Name: "b-usual", InstallationID: "ins_one", Labels: store.StringSlice{"gpu"}, Enabled: true}

	if got := BestPool([]*store.Pool{bare, usual}, job("self-hosted", "gpu")); got == nil || got.Name != "b-usual" {
		t.Fatalf("BestPool = %v, want b-usual", got)
	}
	if got := BestPool([]*store.Pool{bare, usual}, job("gpu")); got == nil || got.Name != "a-bare" {
		t.Fatalf("BestPool = %v, want a-bare for a job that names only gpu", got)
	}
	if ok, why := Eligible(bare, job("self-hosted", "gpu")); ok || why == "" {
		t.Fatalf("Eligible = %v, %q; want a refusal with a reason", ok, why)
	}
}
