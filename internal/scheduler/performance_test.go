package scheduler

import (
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/store"
)

// perfHost is a machine of the shape this fleet runs, with the slots it has
// now, and what its runners were measured to use.
func perfHost(name string, cpus int, memMB, diskTotalMB, diskFreeMB int64, capacity, active, samples int, perRunner float64) PerformanceHost {
	return PerformanceHost{
		Host: &store.Host{
			ID: "host_" + name, Name: name, CPUs: cpus, MemoryMB: memMB,
			DiskTotalMB: diskTotalMB, DiskFreeMB: diskFreeMB, Capacity: capacity, ActiveRunners: active,
		},
		Samples: samples, CPUPerRunner: perRunner,
	}
}

// The fleet this was written from: four hosts holding 14 slots at the default
// two cores and four gigabytes, whose runners used about three quarters of a
// core each, and whose jobs spent nearly all of their waiting for a slot.
func slotBoundFleet() PerformanceEvidence {
	peaks := make([]int64, 40)
	for i := range peaks {
		peaks[i] = 900 + int64(i)*10 // a little over a gigabyte at the ninetieth percentile
	}
	return PerformanceEvidence{
		Hosts: []PerformanceHost{
			perfHost("x1", 12, 31240, 1875626, 1662850, 5, 5, 140, 0.52),
			perfHost("precision", 12, 62759, 959270, 760000, 5, 5, 140, 0.72),
			perfHost("dev", 8, 32045, 147628, 100000, 3, 3, 150, 1.4),
			perfHost("small", 4, 15366, 100220, 78000, 1, 1, 150, 1.1),
		},
		Ask:         RunnerAsk{CPUs: 2, MemoryMB: 4096},
		MinimumCPUs: 1, MinimumMemoryMB: 2048,
		WaitSeconds: 1000, CeilingSeconds: 930, PoolMax: 14,
		PeakMemoryMB: peaks,
	}
}

func TestRecommendPerformanceRaisesSlotsOnAFleetThatWaitsForRoomWhileItsCPUsIdle(t *testing.T) {
	rec := RecommendPerformance(slotBoundFleet())
	if rec.Verdict != PerformanceRaise {
		t.Fatalf("verdict = %q (%s), want raise", rec.Verdict, rec.Reason)
	}
	// The measured use, weighted by the minutes each host was sampled, is about
	// 0.95 cores; a share of one and a half times that, rounded up to a quarter,
	// is 1.5, and the memory is the ninetieth percentile with a fifth more, up
	// to the floor the fleet's minimum sets.
	if rec.Recommended.CPUs != 1.5 {
		t.Errorf("recommended CPUs = %v, want 1.5", rec.Recommended.CPUs)
	}
	if rec.Recommended.MemoryMB != 2048 {
		t.Errorf("recommended memory = %d, want the fleet's 2048 MB minimum", rec.Recommended.MemoryMB)
	}
	if rec.SlotsBefore != 14 || rec.SlotsAfter <= rec.SlotsBefore {
		t.Errorf("slots %d -> %d, want a rise from 14", rec.SlotsBefore, rec.SlotsAfter)
	}
	if rec.MaxRunners < rec.SlotsAfter {
		t.Errorf("max runners = %d, below the %d slots it is meant to leave room for", rec.MaxRunners, rec.SlotsAfter)
	}
	for _, h := range rec.Hosts {
		if h.After < h.Before {
			t.Errorf("%s goes from %d to %d slots: a recommendation never takes a host down", h.HostName, h.Before, h.After)
		}
	}
	for _, want := range []string{"waiting", "slots", "up from 14"} {
		if !strings.Contains(rec.Reason, want) {
			t.Errorf("reason %q does not say %q", rec.Reason, want)
		}
	}
	if len(rec.Cautions) == 0 || !strings.Contains(rec.Cautions[0], "keeps the size it was created with") {
		t.Errorf("cautions = %v, want the one about running runners first", rec.Cautions)
	}
}

// A host that cannot hold more runners of the new size than its disk has room
// for stops there and says so, rather than being filled to the point jobs fail
// for want of space.
func TestRecommendPerformanceStopsAHostAtItsFreeDisk(t *testing.T) {
	e := slotBoundFleet()
	// Two slots now and one runner at work, with 30 GB free: the cores would hold
	// four runners at the new size, the disk holds the one at work and two more.
	e.Hosts[2] = perfHost("dev", 8, 32045, 147628, 30000, 2, 1, 150, 1.4)
	rec := RecommendPerformance(e)
	var dev PerformanceHostChange
	for _, h := range rec.Hosts {
		if h.HostName == "dev" {
			dev = h
		}
	}
	if dev.After != 3 || dev.LimitedBy != "disk" {
		t.Fatalf("dev = %+v, want 3 slots limited by disk (1 working + 2 that its free disk holds)", dev)
	}
	if !strings.Contains(dev.Caution, "of disk free") {
		t.Errorf("caution = %q, want the disk named", dev.Caution)
	}
}

func TestRecommendPerformanceSaysNothingWhereTheEvidenceIsThinOrPointsElsewhere(t *testing.T) {
	tests := map[string]struct {
		change  func(*PerformanceEvidence)
		verdict string
		reason  string
	}{
		"no host": {func(e *PerformanceEvidence) { e.Hosts = nil }, PerformanceUnknown, "No connected host"},
		"too few minutes measured": {func(e *PerformanceEvidence) {
			for i := range e.Hosts {
				e.Hosts[i].Samples = 5
			}
		}, PerformanceUnknown, "minutes of load"},
		"a pool sets its own size":                  {func(e *PerformanceEvidence) { e.Ask = RunnerAsk{CPUs: 4, MemoryMB: 8192, Pool: "big-builds"} }, PerformanceKeep, "Pool big-builds sets its own size"},
		"jobs waited for something other than room": {func(e *PerformanceEvidence) { e.CeilingSeconds = 100 }, PerformanceKeep, "would not shorten the wait"},
		"a job was killed for memory":               {func(e *PerformanceEvidence) { e.OOMKilled = 2 }, PerformanceKeep, "memory is the limit"},
		"runners already use what they are given": {func(e *PerformanceEvidence) {
			for i := range e.Hosts {
				e.Hosts[i].CPUPerRunner = 1.9
			}
		}, PerformanceKeep, "no more slots"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			e := slotBoundFleet()
			tt.change(&e)
			rec := RecommendPerformance(e)
			if rec.Verdict != tt.verdict || !strings.Contains(rec.Reason, tt.reason) {
				t.Fatalf("verdict %q, reason %q; want %q mentioning %q", rec.Verdict, rec.Reason, tt.verdict, tt.reason)
			}
			if rec.Recommended != rec.Current || rec.SlotsAfter != rec.SlotsBefore {
				t.Errorf("a %q answer changed something: %+v", rec.Verdict, rec)
			}
		})
	}
}

// Memory is left alone where too few jobs were measured to size it from, so a
// young pool is never given a memory figure from three runs.
func TestRecommendPerformanceLeavesMemoryAloneWithoutJobHistory(t *testing.T) {
	e := slotBoundFleet()
	e.PeakMemoryMB = []int64{500, 600}
	rec := RecommendPerformance(e)
	if rec.Verdict != PerformanceRaise || rec.Recommended.MemoryMB != e.Ask.MemoryMB {
		t.Fatalf("verdict %q, memory %d; want a raise that keeps the current %d MB", rec.Verdict, rec.Recommended.MemoryMB, e.Ask.MemoryMB)
	}
}

// The other pools that follow the default are named, in a stable order, so the
// operator sees what else the change resizes.
func TestRecommendPerformanceNamesThePoolsThatFollowTheDefault(t *testing.T) {
	e := slotBoundFleet()
	e.Followers = []string{"zoomies-windows", "bare-metal"}
	rec := RecommendPerformance(e)
	if strings.Join(rec.Followers, ",") != "bare-metal,zoomies-windows" {
		t.Fatalf("followers = %v, want them sorted", rec.Followers)
	}
	if !strings.Contains(strings.Join(rec.Cautions, " "), "bare-metal, zoomies-windows") {
		t.Errorf("cautions = %v, want the followers named", rec.Cautions)
	}
}
