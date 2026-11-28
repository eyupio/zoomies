package scheduler

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/eyupio/zoomies/internal/naming"
	"github.com/eyupio/zoomies/internal/store"
)

// DiskBlock is a host with spare slots that cannot place a runner of one or
// more enabled pools because of disk. It uses the same ledger as Decide,
// including the working room live runners have already been promised.
type DiskBlock struct {
	HostID      string
	HostName    string
	FreeMB      int64
	ReserveMB   int64
	Allocatable int64
	CommittedMB int64
	RemainingMB int64
	Pools       []DiskPoolBlock
}

// DiskPoolBlock names one affected pool and the charge for its next runner.
type DiskPoolBlock struct {
	ID     string
	Name   string
	NeedMB int64
}

// DiskBlocks judges the disk gate even when another host can serve the pool.
// A full host has lost no spare slot, and a host that could never serve a pool
// must not be told to reclaim disk for it. Unknown disk is no verdict.
func DiskBlocks(hosts []*store.Host, pools []*store.Pool, runners map[string][]*store.Runner, now time.Time) []DiskBlock {
	hs := newHostSet(hosts, pools, runners, now)
	var out []DiskBlock
	for _, h := range hs.hosts {
		alloc := hs.alloc[h.ID]
		if h.Retired || !HostAvailable(h, now) || hs.free[h.ID] <= 0 || !alloc.DiskKnown {
			continue
		}
		block := DiskBlock{
			HostID:      h.ID,
			HostName:    h.Name,
			FreeMB:      h.DiskFreeMB,
			ReserveMB:   max(h.ReserveDiskMB, store.MinHostReserveDiskMB),
			Allocatable: alloc.DiskMB,
			CommittedMB: alloc.DiskMB - hs.left[h.ID].DiskMB,
			RemainingMB: max(hs.left[h.ID].DiskMB, 0),
		}
		for _, p := range pools {
			if !p.Enabled || !HostOffers(h, p) || !HostIsPlatform(h, p) || !HostSelects(h, p) {
				continue
			}
			// Ask whether disk is the thing reclamation could fix, leaving
			// CPU, memory and the host's runner profile exactly as they are.
			probe := *h
			probe.DiskTotalMB, probe.DiskFreeMB = 0, 0
			if !HostFits(&probe, p) {
				continue
			}
			want := MinimumReserve(p, h)
			if !diskFits(hs.left[h.ID], want, alloc) {
				block.Pools = append(block.Pools, DiskPoolBlock{ID: p.ID, Name: p.Name, NeedMB: want.DiskMB})
			}
		}
		if len(block.Pools) == 0 {
			continue
		}
		slices.SortFunc(block.Pools, func(a, b DiskPoolBlock) int { return strings.Compare(a.ID, b.ID) })
		out = append(out, block)
	}
	return out
}

// Detail names quantities only here, beside the rule that refused placement,
// so a host's problem and its notification explain the scheduler's decision.
func (b DiskBlock) Detail() string {
	pools := make([]string, 0, len(b.Pools))
	for _, p := range b.Pools {
		pools = append(pools, fmt.Sprintf("%s (one runner needs %s)", naming.ForSentence(p.Name), formatMB(p.NeedMB)))
	}
	return fmt.Sprintf("%s free; %s host reserve; %s allocatable; %s already promised to live runners; %s left for a new runner. Disk prevents placement for: %s. Running jobs continue; other pools may still fit.",
		formatMB(b.FreeMB), formatMB(b.ReserveMB), formatMB(b.Allocatable),
		formatMB(b.CommittedMB), formatMB(b.RemainingMB), strings.Join(pools, ", "))
}

// diskFits is the disk part of fits, shared with the operator's assessment.
func diskFits(left, want Reservation, known store.HostAllocation) bool {
	return !known.DiskKnown || (known.DiskMB > 0 && left.DiskMB >= want.DiskMB)
}
