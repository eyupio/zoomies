package controller

import (
	"testing"

	"github.com/eyupio/zoomies/internal/backend"
	"github.com/eyupio/zoomies/internal/registrymirror"
)

// What a host's registry mirror has done is the metrics' business and not the
// host view's: the figures move with every pull, and a series of the host's own is
// what lets rate() read an agent restart as a counter reset.

func TestARegistryMirrorReportBecomesSeriesLabelledByHost(t *testing.T) {
	h := newHarness(t)
	one, two := h.hostWith("one"), h.hostWith("two")
	h.mirrorBeat(one.ID, &backend.RegistryMirrorReport{Stats: registrymirror.Stats{
		Requests: 20, Hits: 12, Misses: 6, UpstreamErrors: 2, UpstreamFailingSeconds: 45, Evictions: 3,
		BytesServed: 7000, BytesFetched: 3000, BytesStored: 4096,
	}})
	h.mirrorBeat(two.ID, &backend.RegistryMirrorReport{Stats: registrymirror.Stats{Hits: 1, BytesStored: 10}})

	for _, tt := range []struct {
		name   string
		series string
		labels map[string]string
		want   float64
	}{
		{"hits", "zoomies_registry_mirror_lookups_total", map[string]string{"host": one.ID, "result": "hit"}, 12},
		{"misses", "zoomies_registry_mirror_lookups_total", map[string]string{"host": one.ID, "result": "miss"}, 6},
		{"bytes served", "zoomies_registry_mirror_served_bytes_total", map[string]string{"host": one.ID}, 7000},
		{"bytes fetched", "zoomies_registry_mirror_fetched_bytes_total", map[string]string{"host": one.ID}, 3000},
		{"upstream errors", "zoomies_registry_mirror_upstream_errors_total", map[string]string{"host": one.ID}, 2},
		{"evictions", "zoomies_registry_mirror_evictions_total", map[string]string{"host": one.ID}, 3},
		{"bytes stored", "zoomies_registry_mirror_stored_bytes", map[string]string{"host": one.ID}, 4096},
		{"how long the registry has failed it", "zoomies_registry_mirror_upstream_failing_seconds", map[string]string{"host": one.ID}, 45},
		// Another host's totals are its own series, not added to the first's.
		{"the other host's hits", "zoomies_registry_mirror_lookups_total", map[string]string{"host": two.ID, "result": "hit"}, 1},
		// A result that has not happened is still reported, so a rate has a
		// series to start from.
		{"the other host's misses", "zoomies_registry_mirror_lookups_total", map[string]string{"host": two.ID, "result": "miss"}, 0},
		{"the other host's evictions", "zoomies_registry_mirror_evictions_total", map[string]string{"host": two.ID}, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := gatherValue(t, h.c, tt.series, tt.labels)
			if !ok || got != tt.want {
				t.Errorf("%s%v = %v (present %v), want %v", tt.series, tt.labels, got, ok, tt.want)
			}
		})
	}

	// The totals are the agent's: a later beat replaces them rather than adding.
	h.mirrorBeat(one.ID, &backend.RegistryMirrorReport{Stats: registrymirror.Stats{Hits: 15}})
	if got, _ := gatherValue(t, h.c, "zoomies_registry_mirror_lookups_total", map[string]string{"host": one.ID, "result": "hit"}); got != 15 {
		t.Errorf("hits after the next beat = %v, want the agent's new total 15", got)
	}
}

// A mirror that is on and has been asked for nothing reports zeros, so that the
// first pull is a change from something and not the first sight of a series.
func TestAMirrorThatIsOnButUnusedReportsZeros(t *testing.T) {
	h := newHarness(t)
	host := h.hostWith("builder")
	h.mirrorBeat(host.ID, &backend.RegistryMirrorReport{})
	for _, series := range []string{
		"zoomies_registry_mirror_served_bytes_total", "zoomies_registry_mirror_fetched_bytes_total",
		"zoomies_registry_mirror_upstream_errors_total", "zoomies_registry_mirror_evictions_total",
		"zoomies_registry_mirror_stored_bytes", "zoomies_registry_mirror_upstream_failing_seconds",
	} {
		if got, ok := gatherValue(t, h.c, series, map[string]string{"host": host.ID}); !ok || got != 0 {
			t.Errorf("%s = %v (present %v), want a zero", series, got, ok)
		}
	}
}

// A heartbeat with no block, which is what an agent that predates the mirror or a
// host with it off sends, clears what was held; a deleted host's totals go with it;
// and nothing an agent says can make a series go backwards, because a counter that
// falls is read as a restart and the host's history with it.
func TestMirrorSeriesAreBoundedByTheHostsTheControllerHasAndNeverNegative(t *testing.T) {
	h := newHarness(t)
	host := h.hostWith("one")
	h.mirrorBeat(host.ID, &backend.RegistryMirrorReport{Stats: registrymirror.Stats{Hits: 5, BytesServed: -9, BytesStored: -1, UpstreamFailingSeconds: -3}})
	for series, labels := range map[string]map[string]string{
		"zoomies_registry_mirror_served_bytes_total":       {"host": host.ID},
		"zoomies_registry_mirror_stored_bytes":             {"host": host.ID},
		"zoomies_registry_mirror_upstream_failing_seconds": {"host": host.ID},
	} {
		if got, ok := gatherValue(t, h.c, series, labels); !ok || got < 0 {
			t.Errorf("%s = %v (present %v), want it never below zero", series, got, ok)
		}
	}

	h.mirrorBeat(host.ID, nil)
	if _, ok := gatherValue(t, h.c, "zoomies_registry_mirror_lookups_total", map[string]string{"host": host.ID}); ok {
		t.Error("a heartbeat that reported no mirror left the earlier totals standing")
	}

	h.mirrorBeat(host.ID, &backend.RegistryMirrorReport{Stats: registrymirror.Stats{Hits: 1}})
	if _, err := h.st.DeleteHost(h.ctx, host.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := gatherValue(t, h.c, "zoomies_registry_mirror_lookups_total", map[string]string{"host": host.ID}); ok {
		t.Error("a deleted host is still exported")
	}
	if n := len(h.c.registryMirrorReportsFor(map[string]bool{})); n != 0 {
		t.Errorf("%d host(s) still held in memory after the host was deleted", n)
	}
}
