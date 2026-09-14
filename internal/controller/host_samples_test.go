package controller

import (
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/store"
)

// The minute sampler writes one row per host, and a measurement that has
// gone stale is left out of it rather than carried forward. A host that
// stopped reporting would otherwise draw a flat line at its last reading,
// which is what a healthy, quiet machine draws too -- so the two would be
// indistinguishable on the chart the sample exists to feed.
func TestSamplingRecordsEveryHostAndLeavesStaleMeasurementsAsGaps(t *testing.T) {
	h := newHarness(t)
	fresh := h.host("vm-fresh")
	stale := h.host("vm-stale")
	now := h.c.Now()

	cpu, load, mem := 72.5, 3.2, int64(20_000)
	if err := h.st.SetHostUsage(h.ctx, fresh.ID, store.HostUsage{
		CPUPercent: &cpu, LoadAverage1: &load, MemoryAvailableMB: &mem, SampledAt: now,
	}); err != nil {
		t.Fatalf("SetHostUsage: %v", err)
	}
	old := 99.0
	if err := h.st.SetHostUsage(h.ctx, stale.ID, store.HostUsage{
		CPUPercent: &old, SampledAt: now.Add(-store.HostUsageMaxAge - time.Minute),
	}); err != nil {
		t.Fatalf("SetHostUsage (stale): %v", err)
	}

	if err := h.c.sample(h.ctx); err != nil {
		t.Fatalf("sample: %v", err)
	}
	samples, err := h.st.ListHostSamples(h.ctx, now.Add(-time.Minute), "")
	if err != nil {
		t.Fatalf("ListHostSamples: %v", err)
	}
	if len(samples) != 2 {
		t.Fatalf("samples = %d, want one per host: %+v", len(samples), samples)
	}
	byHost := map[string]store.HostSample{}
	for _, s := range samples {
		byHost[s.HostID] = s
	}
	got := byHost[fresh.ID]
	if got.CPUPercent == nil || *got.CPUPercent != cpu || got.MemoryAvailableMB == nil || *got.MemoryAvailableMB != mem {
		t.Errorf("fresh host sample = %+v, want the measurement it just reported", got)
	}
	if got.Capacity != 4 || got.CPUs != 16 || got.AllocatableCPUs <= 0 {
		t.Errorf("fresh host sample = %+v, want its slots and size carried", got)
	}
	if s := byHost[stale.ID]; s.CPUPercent != nil {
		t.Errorf("stale host sample carries CPU %v, want a gap", *s.CPUPercent)
	}
}

func TestPruningHostSamplesFollowsTheSampleRetention(t *testing.T) {
	h := newHarness(t)
	host := h.host("vm-1")
	now := h.c.Now()
	if err := h.st.RecordHostSamples(h.ctx, []store.HostSample{
		{HostID: host.ID, At: now.Add(-48 * time.Hour)},
		{HostID: host.ID, At: now.Add(-time.Hour)},
	}); err != nil {
		t.Fatalf("RecordHostSamples: %v", err)
	}
	h.c.UpdateConfig(func(c *config.Config) {
		c.Retention.Samples = 24 * time.Hour
	})
	h.c.prune(h.ctx)
	left, err := h.st.ListHostSamples(h.ctx, now.Add(-72*time.Hour), "")
	if err != nil {
		t.Fatalf("ListHostSamples: %v", err)
	}
	if len(left) != 1 {
		t.Fatalf("samples after the prune = %d, want the one inside the window", len(left))
	}
}

// The store keys a sample on the minute and the chart draws a point per
// minute, so the sampler has to land in every minute. It used to fire once
// sixty seconds had passed since the last sample, from a thirty-second
// ticker: a tick that arrived a few milliseconds short skipped, the next one
// wrote, and the rows fell ninety seconds apart -- every third minute on the
// capacity map an empty slot. Jitter is a property of every ticker, so the
// passes here are given exactly that jitter.
func TestSamplingLandsInEveryMinuteWhateverTheTickerJitter(t *testing.T) {
	h := newHarness(t)
	host := h.host("vm-1")
	now := h.c.Now()
	// Start 200ms into a minute, then let the second tick come 100ms early,
	// which is all it took to lose a minute.
	first := now.Truncate(time.Minute).Add(time.Minute + 200*time.Millisecond)
	h.advance(first.Sub(now))
	var last housekeeping
	h.c.housekeep(h.ctx, &last)
	for _, d := range []time.Duration{
		30 * time.Second,
		30*time.Second - 100*time.Millisecond,
		30 * time.Second,
		30 * time.Second,
	} {
		h.advance(d)
		h.c.housekeep(h.ctx, &last)
	}

	// Five passes at :00.2, :30.2, :00.1, :30.1 and :00.1 span three minutes,
	// and every one of them wants a row.
	samples, err := h.st.ListHostSamples(h.ctx, first.Add(-time.Minute), host.ID)
	if err != nil {
		t.Fatalf("ListHostSamples: %v", err)
	}
	var minutes []time.Time
	for _, s := range samples {
		minutes = append(minutes, s.At.UTC())
	}
	if len(minutes) != 3 {
		t.Fatalf("sampled minutes = %v, want one row in each of the three minutes the passes covered", minutes)
	}
	for i, at := range minutes {
		if want := first.UTC().Truncate(time.Minute).Add(time.Duration(i) * time.Minute); !at.Equal(want) {
			t.Errorf("sample %d at %v, want %v: a minute was skipped", i, at, want)
		}
	}
}
