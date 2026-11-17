package registrymirror

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeRegistry is a registry shaped like Docker Hub, as far as the mirror can
// tell: a token service that hands anyone an anonymous token, manifests that
// need that token, and blobs that answer with a redirect to another server, as
// a registry does to hand a download to its content network. It counts what it
// is asked, because what a mirror saves is requests upstream.
// testClient is what tests talk to a mirror with. It has a deadline so that a
// mirror that never answers fails a test instead of hanging the package.
var testClient = &http.Client{Timeout: 10 * time.Second}

type fakeRegistry struct {
	t   *testing.T
	reg *httptest.Server
	cdn *httptest.Server

	mu        sync.Mutex
	blobs     map[string][]byte
	manifests map[string]fakeManifest // repository + "@" + digest
	tags      map[string]string       // repository + ":" + tag -> digest
	private   map[string]bool
	override  map[string]int // request path -> status to answer with
	badBlob   map[string][]byte
	counts    map[string]int
	tokenAuth []string
	cdnAuth   []string
	cdnDelay  time.Duration
	cdnStall  bool
	// cdnChunked makes the content network send a body of unknown length.
	cdnChunked bool
	// cdnStallEmpty makes a stalled download stop before its first byte.
	cdnStallEmpty bool
	// cdnEnded counts stalled downloads the other end gave up on.
	cdnEnded atomic.Int32
	// manifestStall makes manifest bodies stop halfway.
	manifestStall bool
	// tokenStall makes the token service stop halfway through its answer.
	tokenStall bool
	// accepts is the Accept header of each manifest request, in order.
	accepts []string
	// omitDigest makes manifest answers leave out Docker-Content-Digest.
	omitDigest bool
}

type fakeManifest struct {
	body      []byte
	mediaType string
}

func newFakeRegistry(t *testing.T) *fakeRegistry {
	t.Helper()
	f := &fakeRegistry{
		t:         t,
		blobs:     map[string][]byte{},
		manifests: map[string]fakeManifest{},
		tags:      map[string]string{},
		private:   map[string]bool{},
		override:  map[string]int{},
		badBlob:   map[string][]byte{},
		counts:    map[string]int{},
	}
	f.reg = httptest.NewServer(http.HandlerFunc(f.serveRegistry))
	f.cdn = httptest.NewServer(http.HandlerFunc(f.serveCDN))
	t.Cleanup(f.reg.Close)
	t.Cleanup(f.cdn.Close)
	return f
}

func digestOf(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (f *fakeRegistry) addBlob(b []byte) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	d := digestOf(b)
	f.blobs[d] = b
	return d
}

// addManifest publishes a manifest under a tag, or under its digest alone when
// the tag is empty, and returns its digest.
func (f *fakeRegistry) addManifest(repo, tag string, body []byte, mediaType string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	d := digestOf(body)
	f.manifests[repo+"@"+d] = fakeManifest{body: body, mediaType: mediaType}
	if tag != "" {
		f.tags[repo+":"+tag] = d
	}
	return d
}

// addManifestUnder publishes bytes under a digest they do not hash to, which is
// what a corrupted or malicious upstream would do.
func (f *fakeRegistry) addManifestUnder(repo, digest string, body []byte, mediaType string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.manifests[repo+"@"+digest] = fakeManifest{body: body, mediaType: mediaType}
}

func (f *fakeRegistry) setStall(on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cdnStall = on
}

func (f *fakeRegistry) acceptSeen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.accepts...)
}

func (f *fakeRegistry) moveTag(repo, tag, digest string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tags[repo+":"+tag] = digest
}

func (f *fakeRegistry) count(key string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.counts[key]
}

func (f *fakeRegistry) total() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.counts {
		n += c
	}
	return n
}

func (f *fakeRegistry) inc(key string) {
	f.mu.Lock()
	f.counts[key]++
	f.mu.Unlock()
}

func writeRegistryError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"errors": []map[string]string{{"code": code, "message": message}}})
}

func (f *fakeRegistry) serveRegistry(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/token" {
		f.inc("token")
		f.mu.Lock()
		stall := f.tokenStall
		f.mu.Unlock()
		if stall {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"token":"an-unfinished`))
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		f.mu.Lock()
		f.tokenAuth = append(f.tokenAuth, r.Header.Get("Authorization"))
		f.mu.Unlock()
		scope := strings.TrimSuffix(strings.TrimPrefix(r.URL.Query().Get("scope"), "repository:"), ":pull")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"token": "anon-" + scope, "expires_in": 300})
		return
	}
	rest, ok := strings.CutPrefix(r.URL.Path, "/v2/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	var repo, kind, ref string
	for _, k := range []string{"/manifests/", "/blobs/"} {
		if i := strings.LastIndex(rest, k); i >= 0 {
			repo, kind, ref = rest[:i], strings.Trim(k, "/"), rest[i+len(k):]
		}
	}
	if kind == "" {
		http.NotFound(w, r)
		return
	}
	switch kind {
	case "manifests":
		f.inc("manifest " + r.Method)
	default:
		f.inc("blob")
	}
	if r.Header.Get("Authorization") != "Bearer anon-"+repo || f.isPrivate(repo) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="`+f.reg.URL+`/token",service="fake"`)
		writeRegistryError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}
	f.mu.Lock()
	code, forced := f.override[r.URL.Path]
	f.mu.Unlock()
	if forced {
		if code == http.StatusTooManyRequests {
			w.Header().Set("Retry-After", "30")
		}
		writeRegistryError(w, code, "TOOMANYREQUESTS", "forced by the test")
		return
	}
	if kind == "blobs" {
		f.mu.Lock()
		_, have := f.blobs[ref]
		f.mu.Unlock()
		if !have {
			writeRegistryError(w, http.StatusNotFound, "BLOB_UNKNOWN", "no such blob")
			return
		}
		http.Redirect(w, r, f.cdn.URL+"/blob/"+ref, http.StatusTemporaryRedirect)
		return
	}
	f.mu.Lock()
	f.accepts = append(f.accepts, r.Header.Get("Accept"))
	digest := ref
	if !strings.HasPrefix(ref, "sha256:") {
		digest = f.tags[repo+":"+ref]
	}
	m, have := f.manifests[repo+"@"+digest]
	f.mu.Unlock()
	if !have {
		writeRegistryError(w, http.StatusNotFound, "MANIFEST_UNKNOWN", "no such manifest")
		return
	}
	w.Header().Set("Content-Type", m.mediaType)
	if !f.omitDigestNow() {
		w.Header().Set("Docker-Content-Digest", digest)
	}
	w.Header().Set("Content-Length", fmt.Sprint(len(m.body)))
	if r.Method == http.MethodHead {
		return
	}
	if f.manifestStallNow() {
		_, _ = w.Write(m.body[:len(m.body)/2])
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		return
	}
	_, _ = w.Write(m.body)
}

func (f *fakeRegistry) manifestStallNow() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.manifestStall
}

func (f *fakeRegistry) setManifestStall(on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.manifestStall = on
}

func (f *fakeRegistry) omitDigestNow() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.omitDigest
}

func (f *fakeRegistry) isPrivate(repo string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.private[repo]
}

func (f *fakeRegistry) serveCDN(w http.ResponseWriter, r *http.Request) {
	f.inc("cdn")
	digest := strings.TrimPrefix(r.URL.Path, "/blob/")
	f.mu.Lock()
	f.cdnAuth = append(f.cdnAuth, r.Header.Get("Authorization"))
	body := f.blobs[digest]
	if bad, ok := f.badBlob[digest]; ok {
		body = bad
	}
	delay, stall, chunked, empty := f.cdnDelay, f.cdnStall, f.cdnChunked, f.cdnStallEmpty
	f.mu.Unlock()
	if delay > 0 {
		time.Sleep(delay)
	}
	if chunked {
		half := len(body) / 2
		_, _ = w.Write(body[:half])
		w.(http.Flusher).Flush()
		_, _ = w.Write(body[half:])
		return
	}
	w.Header().Set("Content-Length", fmt.Sprint(len(body)))
	if stall {
		if empty {
			w.WriteHeader(http.StatusOK)
		} else {
			_, _ = w.Write(body[:len(body)/2])
		}
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		f.cdnEnded.Add(1)
		return
	}
	http.ServeContent(w, r, "", time.Time{}, strings.NewReader(string(body)))
}

// newTestMirror builds a mirror over a fake registry, in a folder that goes
// with the test, and serves it. The redirect client is a plain one, because the
// fake content network is on the loopback address the real one refuses.
func newTestMirror(t *testing.T, f *fakeRegistry, mutate func(*Config)) (*Mirror, *httptest.Server) {
	t.Helper()
	cfg := Config{
		Upstream:       f.reg.URL,
		TokenURL:       f.reg.URL + "/token",
		Service:        "fake",
		Dir:            t.TempDir(),
		MaxBytes:       1 << 20,
		RedirectClient: &http.Client{},
	}
	if mutate != nil {
		mutate(&cfg)
	}
	m, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	srv := httptest.NewServer(m)
	t.Cleanup(srv.Close)
	return m, srv
}

func do(t *testing.T, method, url string, header http.Header) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range header {
		req.Header[k] = v
	}
	resp, err := testClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading %s %s: %v", method, url, err)
	}
	return resp, body
}

func registryErrorCode(t *testing.T, body []byte) string {
	t.Helper()
	var e struct {
		Errors []struct {
			Code string `json:"code"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &e); err != nil || len(e.Errors) == 0 {
		t.Fatalf("not a registry error body: %q (%v)", body, err)
	}
	return e.Errors[0].Code
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// fakeClock is the clock a test hands the mirror, so a tag's lifetime is
// something a test moves rather than waits for.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock { return &fakeClock{t: time.Unix(1_800_000_000, 0)} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// fetch is do for a goroutine: it reports an error rather than ending the test.
func fetch(url string, header http.Header) ([]byte, int, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	for k, v := range header {
		req.Header[k] = v
	}
	// A deadline, so that a mirror that never answers fails the test instead of
	// hanging it.
	resp, err := testClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	return body, resp.StatusCode, err
}

func mustPath(t *testing.T, s *store, digest string) string {
	t.Helper()
	p, err := s.path(digest)
	if err != nil {
		t.Fatalf("path(%q): %v", digest, err)
	}
	return p
}

func (f *fakeRegistry) setTokenStall(on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tokenStall = on
}
