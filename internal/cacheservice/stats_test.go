package cacheservice

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

// The service decides hits, misses, duplicates and evictions in the places the
// protocol already forces it to, and Stats only reads what those places counted.
// Each scenario runs a real exchange through the HTTP handlers and checks the
// whole snapshot, so a counter bumped on the wrong branch, or twice, is caught
// by the figure beside it as well as by its own.
func TestStatsCountWhatTheServiceDecided(t *testing.T) {
	ten := func(b byte) []byte { return bytes.Repeat([]byte{b}, 10) }
	start := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		opts Options
		run  func(h *harness, advance func(time.Duration))
		want Stats
	}{
		{
			name: "an exact hit, a prefix hit and a miss",
			run: func(h *harness, _ func(time.Duration)) {
				h.save(mainToken, "go-abc", "v1", ten('a'))
				h.restore(mainToken, "go-abc", "v1")
				// A restore key matching as a prefix is still a cache for the job.
				h.restore(mainToken, "go-zzz", "v1", "go-")
				h.restore(mainToken, "node-abc", "v1", "node-")
			},
			want: Stats{RestoreHits: 2, RestoreMisses: 1, PutsStarted: 1, PutsFinalised: 1, Entries: 1, StoredBytes: 10},
		},
		{
			name: "a key already stored or already being saved is a duplicate, not a failure",
			run: func(h *harness, _ func(time.Duration)) {
				h.save(mainToken, "k", "v", ten('a'))
				h.save(mainToken, "k", "v", ten('a'))
				// Reserved and never finished, so the next caller finds it in flight.
				h.call(mainToken, "CreateCacheEntry", map[string]string{"key": "other", "version": "v"})
				h.call(mainToken, "CreateCacheEntry", map[string]string{"key": "other", "version": "v"})
			},
			want: Stats{PutsStarted: 2, PutsFinalised: 1, PutsDuplicate: 2, Entries: 1, StoredBytes: 10},
		},
		{
			name: "an archive of the wrong size is a failed save",
			run: func(h *harness, _ func(time.Duration)) {
				_, out := h.call(mainToken, "CreateCacheEntry", map[string]string{"key": "k", "version": "v"})
				h.put(out["signed_upload_url"].(string), []byte("12345"))
				h.call(mainToken, "FinalizeCacheEntryUpload", map[string]string{"key": "k", "version": "v", "sizeBytes": "99"})
			},
			want: Stats{PutsStarted: 1, PutsFailed: 1},
		},
		{
			name: "an upload that could not be stored is one failure however many calls follow it",
			opts: Options{MaxBytes: 8},
			run: func(h *harness, _ func(time.Duration)) {
				_, out := h.call(mainToken, "CreateCacheEntry", map[string]string{"key": "k", "version": "v"})
				h.put(out["signed_upload_url"].(string), bytes.Repeat([]byte("x"), 100))
				// The toolkit finalises after a failed upload; the reservation is
				// already gone, so this must not count the same save twice.
				h.call(mainToken, "FinalizeCacheEntryUpload", map[string]string{"key": "k", "version": "v", "sizeBytes": "100"})
			},
			want: Stats{PutsStarted: 1, PutsFailed: 1},
		},
		{
			name: "a reservation nobody finished is a failure once it expires",
			opts: Options{PendingTTL: time.Hour},
			run: func(h *harness, advance func(time.Duration)) {
				h.call(mainToken, "CreateCacheEntry", map[string]string{"key": "k", "version": "v"})
				advance(2 * time.Hour)
				// The next reservation is what reclaims the old one.
				h.call(mainToken, "CreateCacheEntry", map[string]string{"key": "k", "version": "v"})
			},
			want: Stats{PutsStarted: 2, PutsFailed: 1},
		},
		{
			name: "evicting to stay under the limit counts entries and bytes and leaves the new one held",
			opts: Options{MaxBytes: 15},
			run: func(h *harness, _ func(time.Duration)) {
				h.save(mainToken, "old", "v", ten('o'))
				h.ageEntries(time.Now().Add(-time.Hour))
				h.save(mainToken, "new", "v", ten('n'))
			},
			want: Stats{PutsStarted: 2, PutsFinalised: 2, Evictions: 1, EvictedBytes: 10, Entries: 1, StoredBytes: 10},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := start
			tt.opts.Now = func() time.Time { return now }
			h := newHarness(t, tt.opts)
			tt.run(h, func(d time.Duration) { now = now.Add(d) })

			got := h.svc.Stats()
			if got != tt.want {
				t.Errorf("Stats() = %+v, want %+v", got, tt.want)
			}
			// Whatever the scenario, a save is counted as started before it can
			// be counted as ended, or an operator's ratio goes above one.
			if got.PutsFinalised+got.PutsFailed > got.PutsStarted {
				t.Errorf("more saves ended (%d) than started (%d)", got.PutsFinalised+got.PutsFailed, got.PutsStarted)
			}
		})
	}
}

// A service started on a folder an earlier run filled reports what it holds
// without having been asked for any of it: the gauges describe the folder, and
// the counters describe this run.
func TestStatsOfAServiceStartedOnAnExistingFolderCountNothingButHoldWhatIsThere(t *testing.T) {
	dir := t.TempDir()
	first := newHarness(t, Options{Dir: dir})
	first.save(mainToken, "k", "v", []byte("12345"))

	again := newHarness(t, Options{Dir: dir})
	want := Stats{Entries: 1, StoredBytes: 5}
	if got := again.svc.Stats(); got != want {
		t.Errorf("Stats() after a restart = %+v, want %+v", got, want)
	}
}

// The snapshot travels in a heartbeat, so its field names are a protocol: an
// older controller ignores a block it does not know, and a newer one must read
// what an agent of this release sends.
func TestStatsKeepTheirJSONFieldNames(t *testing.T) {
	raw, err := json.Marshal(Stats{RestoreHits: 1, RestoreMisses: 2, PutsStarted: 3, PutsFinalised: 4,
		PutsFailed: 5, PutsDuplicate: 6, Evictions: 7, EvictedBytes: 8, Entries: 9, StoredBytes: 10})
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"restore_hits":1,"restore_misses":2,"puts_started":3,"puts_finalised":4,"puts_failed":5,"puts_duplicate":6,"evictions":7,"evicted_bytes":8,"entries":9,"stored_bytes":10}`
	if string(raw) != want {
		t.Errorf("Stats marshals as\n%s\nwant\n%s", raw, want)
	}
}
