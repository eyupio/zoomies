package controller

import (
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/store"
)

// host.overprovisioned judges a slot against a core and 2 GB because that is
// what a runner nobody sized needs to do real work. A pool whose operator set
// a minimum below that has been sized: its runners are accepted at the
// minimum, and a host whose slots each cover it is a host the pool runs on as
// configured. Calling it too small -- the report this test exists for -- sends
// the operator to lower a capacity the fleet is using. The warning stays
// wherever any pool that reaches the host has no such minimum, because that
// pool's runners still get a slot too thin to work in.
func TestAHostWhoseSlotsCoverEveryPoolsMinimumIsNotOverprovisioned(t *testing.T) {
	small := store.Resources{CPUs: 1, MemoryMB: 2048, MinCPUs: 0.5, MinMemoryMB: 1024}
	tests := []struct {
		name  string
		pools []store.Resources
		warn  bool
	}{
		{"with no pool the default judgement stands", nil, true},
		{"a pool with a minimum the slots cover raises nothing", []store.Resources{small}, false},
		{"a pool without a minimum beside it keeps the warning", []store.Resources{small, {CPUs: 1, MemoryMB: 2048}}, true},
		{"a minimum above the slot share keeps the warning", []store.Resources{{CPUs: 1, MemoryMB: 2048, MinMemoryMB: 1536}}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			// Two cores less the reserve and 4 GB less the reserve: one slot
			// at a core and 2 GB, three at half a core and 1 GB.
			h.measuredHost("small", 2, 4096, 3, enforcesEverything)
			inst := h.installation()
			for i, r := range tc.pools {
				p := h.pool(inst, "pool-"+string(rune('a'+i)))
				p.Resources = r
				if err := h.st.UpdatePool(h.ctx, p); err != nil {
					t.Fatal(err)
				}
			}
			got := contains(h.problemCodes(), "host.overprovisioned")
			if got != tc.warn {
				t.Errorf("host.overprovisioned raised = %v, want %v", got, tc.warn)
			}
			if tc.warn && len(tc.pools) > 0 {
				p := h.problem(t, "host.overprovisioned")
				if strings.Contains(p.Fix, "capacity to 3") {
					t.Errorf("fix %q names the capacity the host already has", p.Fix)
				}
			}
		})
	}
}
