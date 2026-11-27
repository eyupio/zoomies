package scheduler

import (
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/store"
)

func TestDiskBlocksUseThePlacementLedgerAndClearAtTheBoundary(t *testing.T) {
	p := testPool("builds")
	h := testHost("host_a", 4, 1)
	h.CPUs, h.MemoryMB = 16, 64*1024
	h.DiskTotalMB, h.DiskFreeMB, h.ReserveDiskMB = 144*1024, 34*1024, 16*1024
	r := testRunner("busy", p, store.RunnerBusy, 0)
	s := snap([]*store.Pool{p}, []*store.Runner{r}, nil, []*store.Host{h})
	blocks := DiskBlocks(s.Hosts, s.Pools, s.Runners, s.Now)
	if len(blocks) != 1 || len(blocks[0].Pools) != 1 {
		t.Fatalf("blocks = %+v, want the host and its pool", blocks)
	}
	b := blocks[0]
	if b.FreeMB != 34*1024 || b.ReserveMB != 16*1024 || b.Allocatable != 18*1024 || b.CommittedMB != 10*1024 || b.RemainingMB != 8*1024 || b.Pools[0].NeedMB != 10*1024 {
		t.Fatalf("disk ledger = %+v", b)
	}
	if hs := newHostSet(s.Hosts, s.Pools, s.Runners, s.Now); diskFits(hs.left[h.ID], MinimumReserve(p, h), hs.alloc[h.ID]) {
		t.Fatal("assessment raised a block the placement gate accepts")
	}
	h.DiskFreeMB = 36 * 1024
	if got := DiskBlocks(s.Hosts, s.Pools, s.Runners, s.Now); len(got) != 0 {
		t.Fatalf("exactly one runner's room must clear the block: %+v", got)
	}
}

func TestDiskBlocksNameOnlyAffectedPoolsAndUseDinDCharges(t *testing.T) {
	small, large := testPool("small"), testPool("large")
	small.Resources.DiskGB = 2
	large.Resources.DiskGB, large.DockerMode = 6, store.DockerDinD
	h := testHost("host_a", 4, 0)
	h.DiskTotalMB, h.DiskFreeMB = 100*1024, 13*1024
	pools := []*store.Pool{small, large}
	blocks := DiskBlocks([]*store.Host{h}, pools, nil, now)
	if len(blocks) != 1 || len(blocks[0].Pools) != 1 || blocks[0].Pools[0].ID != large.ID || blocks[0].Pools[0].NeedMB != 12*1024 {
		t.Fatalf("blocks = %+v, want only the explicitly sized DinD pair", blocks)
	}
	h.DiskFreeMB = 14 * 1024
	if blocks := DiskBlocks([]*store.Host{h}, pools, nil, now); len(blocks) != 0 {
		t.Fatalf("DinD boundary did not clear: %+v", blocks)
	}
}

func TestDiskBlocksIgnoreHostsAndPoolsThatHaveLostNoDiskCapacity(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(*store.Host, *store.Pool)
	}{
		{"unknown disk", func(h *store.Host, p *store.Pool) { h.DiskTotalMB = 0 }},
		{"offline", func(h *store.Host, p *store.Pool) { h.LastHeartbeat = ago(2 * store.HeartbeatTimeout) }},
		{"cordoned", func(h *store.Host, p *store.Pool) { h.Cordoned = true }},
		{"retired", func(h *store.Host, p *store.Pool) { h.Retired = true }},
		{"incompatible", func(h *store.Host, p *store.Pool) { h.Incompatible = true }},
		{"full", func(h *store.Host, p *store.Pool) { h.ActiveRunners = h.Capacity }},
		{"disabled pool", func(h *store.Host, p *store.Pool) { p.Enabled = false }},
		{"other backend", func(h *store.Host, p *store.Pool) { p.Backend = store.BackendPodman }},
		{"other selector", func(h *store.Host, p *store.Pool) { p.HostSelector = store.StringMap{"zone": "elsewhere"} }},
		{"too little CPU", func(h *store.Host, p *store.Pool) { h.CPUs = 2; p.Resources.CPUs = 8 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := testPool("builds")
			h := testHost("host_a", 4, 0)
			h.DiskTotalMB, h.DiskFreeMB = 100*1024, 5*1024
			tc.set(h, p)
			if got := DiskBlocks([]*store.Host{h}, []*store.Pool{p}, nil, now); len(got) != 0 {
				t.Fatalf("unrelated disk problem: %+v", got)
			}
		})
	}
}

func TestPoolDiskBlockageNamesTheHostsBehindItsCount(t *testing.T) {
	p := testPool("builds")
	a, b := testHost("host_a", 4, 0), testHost("host_b", 4, 0)
	for _, h := range []*store.Host{a, b} {
		h.DiskTotalMB, h.DiskFreeMB = 100*1024, 5*1024
	}
	hs := newHostSet([]*store.Host{a, b}, []*store.Pool{p}, nil, now)
	block := hs.why(p)
	for _, want := range []string{"2 low on disk", "host_a", "host_b"} {
		if !strings.Contains(block.what, want) {
			t.Errorf("blockage %q does not contain %q", block.what, want)
		}
	}
}
