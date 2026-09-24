package scheduler

import (
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/store"
)

// The room a pool with a minimum has on a host is the number of runners the
// pass would really place there: as many at the standard size as fit, then one
// more at whatever the machine can spare above the minimum. Counting only the
// standard calls a host that runs a second, smaller runner one that cannot,
// and pool.max_above_room and pool.host_overcommitted then warn about room the
// fleet is using. Counting every runner at the minimum promises the opposite:
// a reduced runner is given all the host can spare, so there is never a
// second one behind it.
func TestHostRoomCountsTheRunnersAMinimumReallyPlaces(t *testing.T) {
	tests := []struct {
		name     string
		memoryMB int64
		pool     *store.Pool
		wantFits int
	}{
		{"a host with room for one standard and one reduced runner holds two", 12 * 1024,
			withMinimum("m", 2, 8*1024, 0, 4*1024), 2},
		{"a host that fits two standard runners exactly holds two", 16 * 1024,
			withMinimum("m", 2, 8*1024, 0, 4*1024), 2},
		{"a leftover below the minimum is not a runner", 10 * 1024,
			withMinimum("m", 2, 8*1024, 0, 4*1024), 1},
		{"a host below the standard holds one runner given all it has, not several at the minimum", 7 * 1024,
			withMinimum("m", 2, 8*1024, 0, 3*1024), 1},
		{"a host below the minimum holds nothing", 3 * 1024,
			withMinimum("m", 2, 8*1024, 0, 4*1024), 0},
		{"a pool with no minimum counts the standard alone", 12 * 1024,
			limited("m", 2, 8*1024), 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := sized("h", 8, 32, hostFor(tc.memoryMB), 500*1024)
			got := HostRoomFor(h, tc.pool)
			if got.Fits != tc.wantFits {
				t.Errorf("HostRoomFor = %+v, want fits %d", got, tc.wantFits)
			}
			// The pass is the arithmetic's witness: the count must be what
			// placement actually does on the same empty machine.
			hs := newHostSet([]*store.Host{h}, []*store.Pool{tc.pool}, nil, now)
			if placed := hs.placeAvoiding(tc.pool, 8, nil); len(placed) != tc.wantFits {
				t.Errorf("the pass placed %d runners, want the %d the room promises", len(placed), tc.wantFits)
			}
		})
	}
}

// When a pool with a minimum is blocked, the resource named is the one short
// of the minimum, not the one short of the standard. A host with memory for a
// reduced runner and no CPU for one is out of CPU; "short of memory" would send
// the operator to add memory the pool could already run in.
func TestABlockedPoolWithAMinimumNamesWhatIsShortOfTheMinimum(t *testing.T) {
	tests := []struct {
		name string
		left Reservation
		want string
	}{
		{"memory above the minimum and CPU below it is short of CPU",
			Reservation{CPUs: 1, MemoryMB: 6 * 1024, DiskMB: 100 * 1024}, "short of CPU"},
		{"CPU above the minimum and memory below it is short of memory",
			Reservation{CPUs: 3, MemoryMB: 3 * 1024, DiskMB: 100 * 1024}, "short of memory"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := sized("h", 8, 32, hostFor(32*1024), 500*1024)
			p := withMinimum("m", 4, 8*1024, 2, 4*1024)
			hs := newHostSet([]*store.Host{h}, []*store.Pool{p}, nil, now)
			hs.left[h.ID] = tc.left
			hs.promisedMemory[h.ID] = tc.left.MemoryMB
			if placed := hs.placeAvoiding(p, 1, nil); len(placed) != 0 {
				t.Fatalf("placed %+v on a host short of the minimum", placed)
			}
			if got := hs.why(p).what; !strings.Contains(got, tc.want) {
				t.Errorf("why = %q, want it to say %q", got, tc.want)
			}
		})
	}
}
