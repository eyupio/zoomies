package registrymirror

import (
	"bytes"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Every digest that reaches the store has been checked on the way in: the route
// and the registry's own header are held to the digest grammar, and nothing is
// kept until its content hashes to the digest it is kept under. The store does
// not rely on that. It builds a path only from something that is a sha256
// digest, so that no caller, present or future, can make it name a file outside
// its own folder.
func TestTheStoreBuildsNoPathFromAnythingButASha256Digest(t *testing.T) {
	st, err := newStore(t.TempDir(), 1<<20, newFakeClock().Now)
	if err != nil {
		t.Fatal(err)
	}
	hostile := []string{
		"",
		"sha256:",
		"sha256:a",
		"sha256:ab",
		"sha256:../../etc/passwd",
		"../../etc/passwd",
		"sha256:" + strings.Repeat("g", 64),
		"sha256:" + strings.Repeat("A", 64),
		"sha256:" + strings.Repeat("a", 63) + "/",
		"sha256:" + strings.Repeat("a", 62) + "..",
		"sha256:" + strings.Repeat("a", 64) + "/..",
		"sha256:" + strings.Repeat("a", 65),
		"sha512:" + strings.Repeat("a", 128),
		"sha256:" + strings.Repeat("a", 31) + string([]byte{0}) + strings.Repeat("a", 32),
		`sha256:..\..\windows\win.ini`,
	}
	for _, digest := range hostile {
		t.Run(digest, func(t *testing.T) {
			if p, err := st.path(digest); err == nil {
				t.Errorf("path(%q) = %q, want an error", digest, p)
			}
		})
	}

	good := digestOf([]byte("fine"))
	p, err := st.path(good)
	if err != nil {
		t.Fatalf("path(%q): %v", good, err)
	}
	rel, err := filepath.Rel(filepath.Join(st.dir, "objects"), p)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		t.Errorf("path(%q) = %q, which is not inside the store's objects folder", good, p)
	}
}

// A commit under a digest that is not one writes nothing, anywhere: not under
// the store, and not beside it.
func TestACommitUnderADigestThatIsNotOneCreatesNothing(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "store")
	st, err := newStore(dir, 1<<20, newFakeClock().Now)
	if err != nil {
		t.Fatal(err)
	}
	for _, digest := range []string{"sha256:../../escape", "../escape", "sha256:" + strings.Repeat("a", 62) + ".."} {
		p, err := st.begin()
		if err != nil {
			t.Fatal(err)
		}
		_, _ = p.Write([]byte("content"))

		if err := p.commit(digest, "text/plain"); err == nil {
			t.Errorf("a commit under %q was accepted", digest)
		}
	}

	before, _ := os.ReadDir(parent)
	if len(before) != 1 || before[0].Name() != "store" {
		t.Errorf("the folder beside the store holds %v, want only the store", before)
	}
	objects, _ := filepath.Glob(filepath.Join(dir, "objects", "*"))
	if len(objects) != 0 {
		t.Errorf("objects were created: %v", objects)
	}
	if st.stored() != 0 {
		t.Errorf("%d bytes counted", st.stored())
	}
}

// A folder can hold things the mirror did not put there: a copy that was
// interrupted, a file an operator left, a name from some other layout. Only what
// is named as the mirror names its own objects, in the place it would put them,
// is an object. The rest is left alone, and uncounted, and cannot be evicted.
func TestAStartTreatsOnlyWhatIsNamedAsAnObjectAsOne(t *testing.T) {
	f := newFakeRegistry(t)
	dir := t.TempDir()
	blob := f.addBlob(blobOf(500, 'k'))
	first, srv := newTestMirror(t, f, func(c *Config) { c.Dir = dir })
	do(t, http.MethodGet, srv.URL+"/v2/library/alpine/blobs/"+blob, nil)
	real := first.Stats().BytesStored
	srv.Close()

	wrongShard := strings.Repeat("c", 64)
	junk := map[string]string{
		filepath.Join("objects", "zz", "README"):                           "not an object",
		filepath.Join("objects", "ab", "c"):                                "one character",
		filepath.Join("objects", "ab", "x"):                                "another",
		filepath.Join("objects", "ab", strings.Repeat("g", 64)):            "not hex",
		filepath.Join("objects", "ab", strings.Repeat("A", 64)):            "upper case",
		filepath.Join("objects", "dd", wrongShard):                         "a digest in the wrong shard",
		filepath.Join("objects", "ab", strings.Repeat("a", 64)+".partial"): "a copy that was cut short",
	}
	for name, content := range junk {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
			t.Fatal(err)
		}
	}

	second, srv2 := newTestMirror(t, f, func(c *Config) { c.Dir = dir; c.MaxBytes = 1200 })

	if got := second.Stats().BytesStored; got != real {
		t.Errorf("%d bytes counted after the start, want %d: only the real object", got, real)
	}
	// Fill the store past its bound so that eviction runs over everything it
	// knows about. Nothing it did not write may be touched, and nothing may panic.
	for _, fill := range []byte("MNO") {
		digest := f.addBlob(blobOf(300, fill))
		do(t, http.MethodGet, srv2.URL+"/v2/library/alpine/blobs/"+digest, nil)
	}
	for name := range junk {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s was removed by a mirror that did not write it: %v", name, err)
		}
	}
	if got := second.Stats().BytesStored; got > 1200 {
		t.Errorf("%d bytes counted, over the bound of 1200", got)
	}
}

func commitContent(t *testing.T, st *store, content []byte) string {
	t.Helper()
	digest := digestOf(content)
	p, err := st.begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := p.commit(digest, ""); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return digest
}

// Nothing can be evicted while every object is being read, so a commit then
// leaves the store over its bound. That is allowed to be temporary, and it has
// to be: once the readers are done the store comes back within its bound, and
// does not wait for the next commit to notice.
func TestTheStoreComesBackWithinItsBoundWhenWhatPinnedItIsReleased(t *testing.T) {
	clock := newFakeClock()
	st, err := newStore(t.TempDir(), 4000, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	var digests []string
	for _, fill := range []byte("ABCD") {
		digests = append(digests, commitContent(t, st, blobOf(1000, fill)))
		clock.advance(time.Second)
	}
	var reading []held
	for _, d := range digests {
		obj, ok := st.open(d)
		if !ok {
			t.Fatalf("%s is not held", d[:19])
		}
		reading = append(reading, obj)
	}

	commitContent(t, st, blobOf(1000, 'E'))
	if got := st.stored(); got != 5000 {
		t.Fatalf("%d bytes while four objects were being read, want 5000: nothing could be evicted", got)
	}
	for _, obj := range reading {
		obj.release()
	}

	if got := st.stored(); got > 4000 {
		t.Errorf("%d bytes after every reader was done, over the bound of 4000", got)
	}
}

// What blobs are held to, a manifest is held to as well. An object over a
// quarter of the bound is not kept, whatever it is, because keeping it would
// mean evicting a quarter of everything else for it.
func TestTheStoreRefusesToKeepAnObjectOverAQuarterOfItsBound(t *testing.T) {
	st, err := newStore(t.TempDir(), 4000, newFakeClock().Now)
	if err != nil {
		t.Fatal(err)
	}
	kept := commitContent(t, st, blobOf(500, 'k'))
	content := blobOf(1001, 'o')
	p, err := st.begin()
	if err != nil {
		t.Fatal(err)
	}
	_, _ = p.Write(content)

	if err := p.commit(digestOf(content), ""); err == nil {
		t.Error("an object over a quarter of the bound was kept")
	}

	if got := st.stored(); got != 500 {
		t.Errorf("%d bytes after the refusal, want 500: what was held stays", got)
	}
	if _, ok := st.open(kept); !ok {
		t.Error("the refusal evicted what was held")
	}
}

func TestAManifestOverAQuarterOfTheBoundIsServedAndNotKept(t *testing.T) {
	f := newFakeRegistry(t)
	body := bytes.Repeat([]byte("m"), 1500)
	digest := f.addManifest("library/alpine", "", body, dockerV2)
	kept := f.addBlob(blobOf(500, 'b'))
	var logs bytes.Buffer
	m, srv := newTestMirror(t, f, func(c *Config) {
		c.MaxBytes = 4000
		c.Log = slog.New(slog.NewTextHandler(&logs, nil))
	})
	do(t, http.MethodGet, srv.URL+"/v2/library/alpine/blobs/"+kept, nil)
	url := srv.URL + "/v2/library/alpine/manifests/" + digest

	resp, got := do(t, http.MethodGet, url, nil)
	do(t, http.MethodGet, url, nil)

	if resp.StatusCode != http.StatusOK || !bytes.Equal(got, body) {
		t.Fatalf("status %d, %d bytes: a manifest too large to keep is still served", resp.StatusCode, len(got))
	}
	if got := m.Stats().BytesStored; got != 500 {
		t.Errorf("%d bytes kept, want 500: the manifest is not kept and what was held stays", got)
	}
	if n := f.count("manifest GET"); n != 2 {
		t.Errorf("upstream was asked %d times, want 2: it was not kept", n)
	}
	// Too large to keep is an ordinary outcome and not a fault, so it is not
	// logged as one: a warning means somebody should look.
	if logs.Len() != 0 {
		t.Errorf("an ordinary outcome was logged: %s", logs.String())
	}
}
