package registrymirror

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fetchBlob asks a mirror for a blob and moves the test's clock on, so that the
// order things were used in is the order the store sees them in.
func fetchBlob(t *testing.T, srv string, clock *fakeClock, digest string) {
	t.Helper()
	resp, _ := do(t, http.MethodGet, srv+"/v2/library/alpine/blobs/"+digest, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("blob %s: status %d", digest[:19], resp.StatusCode)
	}
	clock.advance(time.Second)
}

func TestTheStoreEvictsTheLeastRecentlyUsedBlobPastItsBound(t *testing.T) {
	f := newFakeRegistry(t)
	clock := newFakeClock()
	var digests []string
	for _, fill := range []byte("ABCDE") {
		digests = append(digests, f.addBlob(blobOf(700, fill)))
	}
	a, b, c, d, e := digests[0], digests[1], digests[2], digests[3], digests[4]
	m, srv := newTestMirror(t, f, func(cfg *Config) { cfg.MaxBytes = 3000; cfg.Now = clock.Now })

	for _, digest := range []string{a, b, c, d} {
		fetchBlob(t, srv.URL, clock, digest)
	}
	if got := m.Stats().BytesStored; got != 2800 {
		t.Fatalf("%d bytes kept after four blobs, want 2800: all four fit", got)
	}
	fetchBlob(t, srv.URL, clock, a) // a is now more recent than b, c and d
	fetchBlob(t, srv.URL, clock, e) // 3500 bytes: one blob has to go

	got := m.Stats()
	if got.BytesStored > 3000 {
		t.Errorf("%d bytes kept, over the bound of 3000", got.BytesStored)
	}
	if got.Evictions != 1 {
		t.Errorf("%d evictions, want 1", got.Evictions)
	}
	before := f.count("cdn")
	fetchBlob(t, srv.URL, clock, a)
	fetchBlob(t, srv.URL, clock, e)
	if f.count("cdn") != before {
		t.Error("a recently used blob was evicted")
	}
	fetchBlob(t, srv.URL, clock, b)
	if f.count("cdn") != before+1 {
		t.Error("the least recently used blob was not the one evicted")
	}
}

// Something being served is something a client is reading. On a platform that
// refuses to delete an open file that would be an error, and on one that does
// not it would still be a bound that the store stays under only by luck.
func TestTheStoreNeverEvictsABlobBeingServed(t *testing.T) {
	f := newFakeRegistry(t)
	clock := newFakeClock()
	var digests []string
	for _, fill := range []byte("ABCDE") {
		digests = append(digests, f.addBlob(blobOf(700, fill)))
	}
	m, srv := newTestMirror(t, f, func(cfg *Config) { cfg.MaxBytes = 3000; cfg.Now = clock.Now })
	fetchBlob(t, srv.URL, clock, digests[0])
	reading, ok := m.store.open(digests[0])
	if !ok {
		t.Fatal("the first blob is not held")
	}

	for _, digest := range digests[1:] {
		fetchBlob(t, srv.URL, clock, digest)
	}

	if _, err := os.Stat(mustPath(t, m.store, digests[0])); err != nil {
		t.Errorf("the blob being read was evicted: %v", err)
	}
	reading.release()
	fetchBlob(t, srv.URL, clock, digests[0])
	fetchBlob(t, srv.URL, clock, digests[1])
	if got := m.Stats().BytesStored; got > 3000 {
		t.Errorf("%d bytes kept once nothing was being read, over the bound of 3000", got)
	}
}

// One enormous image must not be able to push everything else out. A blob over
// a quarter of the bound is sent on and not kept; one exactly a quarter is.
func TestABlobOverAQuarterOfTheBoundIsStreamedAndNotKept(t *testing.T) {
	for _, c := range []struct {
		name    string
		size    int
		chunked bool
		kept    bool
	}{
		{"exactly a quarter", 1000, false, true},
		{"one byte over", 1001, false, false},
		// A registry may send a blob with no length, and the rule has to hold as
		// the bytes arrive rather than from a header that is not there.
		{"of unknown length, exactly a quarter", 1000, true, true},
		{"of unknown length, over a quarter", 3000, true, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFakeRegistry(t)
			f.cdnChunked = c.chunked
			data := blobOf(c.size, 'q')
			digest := f.addBlob(data)
			m, srv := newTestMirror(t, f, func(cfg *Config) { cfg.MaxBytes = 4000 })
			url := srv.URL + "/v2/library/alpine/blobs/" + digest

			_, first := do(t, http.MethodGet, url, nil)
			do(t, http.MethodGet, url, nil)

			if len(first) != c.size {
				t.Fatalf("the client got %d bytes, want %d: a blob that is not kept is still served", len(first), c.size)
			}
			wantStored, wantFetches := int64(0), 2
			if c.kept {
				wantStored, wantFetches = int64(c.size), 1
			}
			if got := m.Stats().BytesStored; got != wantStored {
				t.Errorf("%d bytes kept, want %d", got, wantStored)
			}
			if n := f.count("cdn"); n != wantFetches {
				t.Errorf("the content network was asked %d times, want %d", n, wantFetches)
			}
		})
	}
}

// A crash leaves half-written files, and a restart has to neither serve them nor
// forget how much room is already used, or the bound would not survive one.
func TestAStartSweepsLeftoversAndCountsWhatIsOnDisk(t *testing.T) {
	f := newFakeRegistry(t)
	dir := t.TempDir()
	blob := f.addBlob(blobOf(500, 'k'))
	manifest := f.addManifest("library/alpine", "", manifestBody("kept"), ociIndex)
	first, srv := newTestMirror(t, f, func(c *Config) { c.Dir = dir })
	do(t, http.MethodGet, srv.URL+"/v2/library/alpine/blobs/"+blob, nil)
	do(t, http.MethodGet, srv.URL+"/v2/library/alpine/manifests/"+manifest, nil)
	kept := first.Stats().BytesStored
	srv.Close()

	stray := filepath.Join(dir, "tmp", "obj-crashed.tmp")
	orphanType := mustPath(t, first.store, digestOf([]byte("gone"))) + typeSuffix
	if err := os.WriteFile(stray, []byte("half"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(orphanType), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(orphanType, []byte("text/plain"), 0o640); err != nil {
		t.Fatal(err)
	}

	second, srv2 := newTestMirror(t, f, func(c *Config) { c.Dir = dir })

	if _, err := os.Stat(stray); !os.IsNotExist(err) {
		t.Errorf("a temporary file survived the start: %v", err)
	}
	if _, err := os.Stat(orphanType); !os.IsNotExist(err) {
		t.Errorf("a content type with no object survived the start: %v", err)
	}
	if got := second.Stats().BytesStored; got != kept {
		t.Errorf("%d bytes counted after the restart, want %d", got, kept)
	}
	before := f.total()
	resp, _ := do(t, http.MethodGet, srv2.URL+"/v2/library/alpine/manifests/"+manifest, nil)
	do(t, http.MethodGet, srv2.URL+"/v2/library/alpine/blobs/"+blob, nil)
	if f.total() != before {
		t.Errorf("what was kept before the restart was fetched again: %d requests upstream", f.total()-before)
	}
	if ct := resp.Header.Get("Content-Type"); ct != ociIndex {
		t.Errorf("a manifest kept before the restart is served as %q, want %q", ct, ociIndex)
	}
}

// Lowering the bound has to take effect on what is already there.
func TestAStartWithASmallerBoundEvictsDownToIt(t *testing.T) {
	f := newFakeRegistry(t)
	dir := t.TempDir()
	_, srv := newTestMirror(t, f, func(c *Config) { c.Dir = dir; c.MaxBytes = 10_000 })
	for _, fill := range []byte("ABCD") {
		digest := f.addBlob(blobOf(900, fill))
		do(t, http.MethodGet, srv.URL+"/v2/library/alpine/blobs/"+digest, nil)
	}
	srv.Close()

	smaller, _ := newTestMirror(t, f, func(c *Config) { c.Dir = dir; c.MaxBytes = 2000 })

	if got := smaller.Stats().BytesStored; got > 2000 {
		t.Errorf("%d bytes kept after the bound was lowered to 2000", got)
	}
	if smaller.Stats().Evictions == 0 {
		t.Error("nothing was evicted")
	}
	entries, _ := filepath.Glob(filepath.Join(dir, "objects", "*", "*"))
	var left int
	for _, e := range entries {
		if !strings.HasSuffix(e, typeSuffix) {
			left++
		}
	}
	if left > 2 {
		t.Errorf("%d object files on disk, want at most 2", left)
	}
}
