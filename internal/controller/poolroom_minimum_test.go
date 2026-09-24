package controller

import (
	"testing"

	"github.com/eyupio/zoomies/internal/store"
)

// A host smaller than a pool's standard size but above its minimum is a host
// the pool runs on, and its line in the room says so at the size a runner
// there is really given. Quoting the standard charged, or no room at all, is
// how an operator was told a host the fleet was using was too small for it.
func TestPoolRoomCountsAHostThatFitsOnlyAtTheMinimum(t *testing.T) {
	tests := []struct {
		name        string
		memoryMB    int64
		capacity    int
		maxRunners  int
		wantFits    int
		wantCharged func(allocMB int64) int64
		wantWarn    bool
	}{
		{name: "a host below the standard holds one runner given all its memory",
			memoryMB: 8 * 1024, capacity: 1, maxRunners: 1, wantFits: 1,
			wantCharged: func(a int64) int64 { return a }},
		{name: "a host with room for one standard and one reduced runner is not over-committed at two slots",
			memoryMB: 14 * 1024, capacity: 2, maxRunners: 2, wantFits: 2,
			wantCharged: func(int64) int64 { return 8 * 1024 }},
		{name: "a maximum above what the minimum can place is still warned about",
			memoryMB: 8 * 1024, capacity: 4, maxRunners: 4, wantFits: 1,
			wantCharged: func(a int64) int64 { return a }, wantWarn: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := &store.Host{ID: "host_a", Name: "small-1", Capacity: tc.capacity,
				CPUs: 32, MemoryMB: tc.memoryMB, Backends: []string{"docker"}}
			alloc := h.Allocatable().MemoryMB
			p := &store.Pool{ID: "pool_m", Name: "standard", Enabled: true, Backend: store.BackendDocker,
				MaxRunners: tc.maxRunners,
				Resources:  store.Resources{CPUs: 2, MemoryMB: 8 * 1024, MinMemoryMB: 4 * 1024}}

			entry := poolHostRoom(h, p)
			if entry.Fits != tc.wantFits {
				t.Errorf("fits = %d, want %d", entry.Fits, tc.wantFits)
			}
			if want := tc.wantCharged(alloc); entry.ChargeMemoryMB != want {
				t.Errorf("charged %d MB, want %d MB", entry.ChargeMemoryMB, want)
			}

			room := PoolRoom{Runners: entry.Room, Slots: entry.Slots, Hosts: []PoolHostRoom{entry}}
			ws := PoolRoomWarnings(p, room)
			if got := warned(ws, "pool.max_above_room") != nil; got != tc.wantWarn {
				t.Errorf("pool.max_above_room raised = %v, want %v", got, tc.wantWarn)
			}
			if w := warned(ws, "pool.host_overcommitted"); w != nil && !tc.wantWarn {
				t.Errorf("a host the pool fills at its minimum was called over-committed: %s", w.Detail)
			}
		})
	}
}
