package registrymirror

import (
	"bytes"
	"fmt"
	"net/http"
	"testing"
	"time"
)

const (
	dockerV2 = "application/vnd.docker.distribution.manifest.v2+json"
	ociIndex = "application/vnd.oci.image.index.v1+json"
)

func manifestBody(tag string) []byte {
	return []byte(fmt.Sprintf(`{"schemaVersion":2,"mediaType":%q,"note":%q}`, dockerV2, tag))
}

func accept(types ...string) http.Header {
	h := http.Header{}
	for _, t := range types {
		h.Add("Accept", t)
	}
	return h
}

func TestAManifestFetchedByDigestIsKeptAndServedFromDisk(t *testing.T) {
	f := newFakeRegistry(t)
	body := manifestBody("one")
	digest := f.addManifest("library/alpine", "", body, ociIndex)
	m, srv := newTestMirror(t, f, nil)
	url := srv.URL + "/v2/library/alpine/manifests/" + digest

	first, got1 := do(t, http.MethodGet, url, accept(dockerV2))
	second, got2 := do(t, http.MethodGet, url, accept(dockerV2))

	for i, r := range []*http.Response{first, second} {
		if r.StatusCode != http.StatusOK {
			t.Fatalf("request %d: status %d", i+1, r.StatusCode)
		}
		if ct := r.Header.Get("Content-Type"); ct != ociIndex {
			t.Errorf("request %d: Content-Type %q, want %q: the type is the manifest's, kept with it", i+1, ct, ociIndex)
		}
		if d := r.Header.Get("Docker-Content-Digest"); d != digest {
			t.Errorf("request %d: Docker-Content-Digest %q, want %q", i+1, d, digest)
		}
	}
	if !bytes.Equal(got1, body) || !bytes.Equal(got2, body) {
		t.Fatal("a body did not match what upstream holds")
	}
	if n := f.count("manifest GET"); n != 1 {
		t.Errorf("upstream was asked for the manifest %d times, want 1", n)
	}
	if got := m.Stats().BytesStored; got != int64(len(body)) {
		t.Errorf("%d bytes kept, want %d", got, len(body))
	}
}

// A manifest is small, so unlike a blob it can be checked before a byte is
// sent. Bytes that do not hash to the digest they were asked for are refused
// and kept nowhere.
func TestAManifestWhoseBytesDoNotHashToItsDigestIsRefused(t *testing.T) {
	f := newFakeRegistry(t)
	claimed := digestOf([]byte("what the digest says"))
	f.addManifestUnder("library/alpine", claimed, manifestBody("forged"), dockerV2)
	m, srv := newTestMirror(t, f, nil)

	resp, body := do(t, http.MethodGet, srv.URL+"/v2/library/alpine/manifests/"+claimed, nil)

	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status %d, want 502", resp.StatusCode)
	}
	if got := registryErrorCode(t, body); got != "UNAVAILABLE" {
		t.Errorf("code %s, want UNAVAILABLE", got)
	}
	if got := m.Stats().BytesStored; got != 0 {
		t.Errorf("%d bytes kept", got)
	}
}

// What is kept is bounded by the folder, but what is held in memory while it is
// checked is bounded by this: a job could otherwise ask for a huge "manifest"
// many times over.
func TestAManifestOverTheSizeLimitIsRefused(t *testing.T) {
	f := newFakeRegistry(t)
	huge := bytes.Repeat([]byte("x"), maxManifestBytes+1)
	digest := f.addManifest("library/alpine", "", huge, dockerV2)
	m, srv := newTestMirror(t, f, nil)

	resp, _ := do(t, http.MethodGet, srv.URL+"/v2/library/alpine/manifests/"+digest, nil)

	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status %d, want 502", resp.StatusCode)
	}
	if got := m.Stats().BytesStored; got != 0 {
		t.Errorf("%d bytes kept", got)
	}
}

// What a client accepts is passed to the registry as it was given, because the
// registry decides what to answer with and the mirror does not know the types.
func TestTheClientsAcceptHeaderIsPassedToTheRegistryAsGiven(t *testing.T) {
	f := newFakeRegistry(t)
	digest := f.addManifest("library/alpine", "latest", manifestBody("a"), dockerV2)
	_, srv := newTestMirror(t, f, nil)

	do(t, http.MethodGet, srv.URL+"/v2/library/alpine/manifests/"+digest, accept(dockerV2, ociIndex))

	want := dockerV2 + ", " + ociIndex
	if seen := f.acceptSeen(); len(seen) != 1 || seen[0] != want {
		t.Errorf("registry saw Accept %q, want %q: both of the client's types, in its order", seen, want)
	}
}

func TestAManifestRequestedByTagIsResolvedOnceAndThenServedFromTheMirror(t *testing.T) {
	f := newFakeRegistry(t)
	body := manifestBody("latest")
	digest := f.addManifest("library/alpine", "latest", body, dockerV2)
	_, srv := newTestMirror(t, f, nil)
	url := srv.URL + "/v2/library/alpine/manifests/latest"

	resp, got := do(t, http.MethodGet, url, accept(dockerV2))
	if resp.StatusCode != http.StatusOK || !bytes.Equal(got, body) {
		t.Fatalf("status %d, body %q", resp.StatusCode, got)
	}
	if d := resp.Header.Get("Docker-Content-Digest"); d != digest {
		t.Errorf("Docker-Content-Digest %q, want %q", d, digest)
	}
	if f.count("manifest HEAD") != 1 || f.count("manifest GET") != 1 {
		t.Fatalf("upstream saw %d HEAD and %d GET, want 1 and 1", f.count("manifest HEAD"), f.count("manifest GET"))
	}

	do(t, http.MethodGet, url, accept(dockerV2))

	if f.count("manifest HEAD") != 1 || f.count("manifest GET") != 1 {
		t.Errorf("a second request inside the tag's lifetime reached upstream: %d HEAD, %d GET", f.count("manifest HEAD"), f.count("manifest GET"))
	}
}

// dockerd asks for a tag with a HEAD, and then for the manifest by digest. The
// HEAD is answered by asking upstream the same, which is cheap, and does not
// fetch the manifest; the GET that follows does.
func TestAHeadForATagIsAnsweredWithoutFetchingTheManifest(t *testing.T) {
	f := newFakeRegistry(t)
	body := manifestBody("latest")
	digest := f.addManifest("library/alpine", "latest", body, dockerV2)
	_, srv := newTestMirror(t, f, nil)

	resp, got := do(t, http.MethodHead, srv.URL+"/v2/library/alpine/manifests/latest", accept(dockerV2))

	if resp.StatusCode != http.StatusOK || len(got) != 0 {
		t.Fatalf("status %d with %d body bytes, want 200 and none", resp.StatusCode, len(got))
	}
	if d := resp.Header.Get("Docker-Content-Digest"); d != digest {
		t.Errorf("Docker-Content-Digest %q, want %q", d, digest)
	}
	if ct := resp.Header.Get("Content-Type"); ct != dockerV2 {
		t.Errorf("Content-Type %q, want %q", ct, dockerV2)
	}
	if cl := resp.Header.Get("Content-Length"); cl != fmt.Sprint(len(body)) {
		t.Errorf("Content-Length %q, want %d", cl, len(body))
	}
	if f.count("manifest GET") != 0 {
		t.Fatalf("a HEAD fetched the manifest %d times", f.count("manifest GET"))
	}

	do(t, http.MethodGet, srv.URL+"/v2/library/alpine/manifests/"+digest, accept(dockerV2))

	if f.count("manifest GET") != 1 {
		t.Errorf("the GET by digest reached upstream %d times, want 1", f.count("manifest GET"))
	}
}

// A tag is a pointer and it moves, so it is trusted for a while and then asked
// about again. The question is a HEAD, which is what makes it cheap, and a
// manifest is fetched only when the answer is a digest the mirror does not hold.
func TestATagIsTrustedForItsLifetimeThenRevalidatedWithAHead(t *testing.T) {
	f := newFakeRegistry(t)
	clock := newFakeClock()
	oldDigest := f.addManifest("library/alpine", "latest", manifestBody("old"), dockerV2)
	_, srv := newTestMirror(t, f, func(c *Config) { c.Now = clock.Now; c.TagTTL = 5 * time.Minute })
	url := srv.URL + "/v2/library/alpine/manifests/latest"

	do(t, http.MethodGet, url, accept(dockerV2))
	clock.advance(4 * time.Minute)
	do(t, http.MethodGet, url, accept(dockerV2))
	if f.count("manifest HEAD") != 1 {
		t.Fatalf("a tag was asked about again after 4 of 5 minutes: %d HEADs", f.count("manifest HEAD"))
	}

	clock.advance(2 * time.Minute)
	_, body := do(t, http.MethodGet, url, accept(dockerV2))
	if f.count("manifest HEAD") != 2 || f.count("manifest GET") != 1 {
		t.Fatalf("after the lifetime: %d HEAD and %d GET, want 2 and 1: the same digest needs no fetch", f.count("manifest HEAD"), f.count("manifest GET"))
	}
	if !bytes.Equal(body, manifestBody("old")) {
		t.Fatalf("body %q", body)
	}

	newDigest := f.addManifest("library/alpine", "", manifestBody("new"), dockerV2)
	f.moveTag("library/alpine", "latest", newDigest)
	clock.advance(6 * time.Minute)
	resp, body := do(t, http.MethodGet, url, accept(dockerV2))

	if !bytes.Equal(body, manifestBody("new")) {
		t.Errorf("body %q after the tag moved, want the new manifest", body)
	}
	if d := resp.Header.Get("Docker-Content-Digest"); d == oldDigest || d != newDigest {
		t.Errorf("Docker-Content-Digest %q, want %q", d, newDigest)
	}
	if f.count("manifest GET") != 2 {
		t.Errorf("%d manifest GETs, want 2: the new digest has to be fetched once", f.count("manifest GET"))
	}
}

// Whether a registry answers a tag with one manifest or another can depend on
// what the client says it accepts, so what is remembered about a tag is
// remembered for that.
func TestATagIsRememberedSeparatelyForWhatTheClientAccepts(t *testing.T) {
	f := newFakeRegistry(t)
	f.addManifest("library/alpine", "latest", manifestBody("a"), dockerV2)
	_, srv := newTestMirror(t, f, nil)
	url := srv.URL + "/v2/library/alpine/manifests/latest"

	do(t, http.MethodHead, url, accept(dockerV2))
	do(t, http.MethodHead, url, accept(dockerV2))
	if n := f.count("manifest HEAD"); n != 1 {
		t.Fatalf("the same Accept asked upstream %d times, want 1", n)
	}
	do(t, http.MethodHead, url, accept(ociIndex))

	if n := f.count("manifest HEAD"); n != 2 {
		t.Errorf("a different Accept shared the first one's answer: %d HEADs, want 2", n)
	}
}

func TestAnAbsentTagIsAnswered404AndNotRemembered(t *testing.T) {
	f := newFakeRegistry(t)
	_, srv := newTestMirror(t, f, nil)
	url := srv.URL + "/v2/library/alpine/manifests/nope"

	resp, body := do(t, http.MethodGet, url, nil)
	do(t, http.MethodGet, url, nil)

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status %d, want 404", resp.StatusCode)
	}
	if got := registryErrorCode(t, body); got != "MANIFEST_UNKNOWN" {
		t.Errorf("code %s, want MANIFEST_UNKNOWN", got)
	}
	if n := f.count("manifest HEAD"); n != 2 {
		t.Errorf("%d HEADs for two requests, want 2: an absence is not remembered, because it may be published a minute later", n)
	}
}

func TestAPrivateRepositoryManifestIsNeverKept(t *testing.T) {
	f := newFakeRegistry(t)
	digest := f.addManifest("acme/secret", "latest", manifestBody("s"), dockerV2)
	f.private["acme/secret"] = true
	m, srv := newTestMirror(t, f, nil)

	for _, ref := range []string{"latest", digest} {
		resp, body := do(t, http.MethodGet, srv.URL+"/v2/acme/secret/manifests/"+ref, nil)

		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s: status %d, want 404", ref, resp.StatusCode)
		}
		if got := registryErrorCode(t, body); got != "NAME_UNKNOWN" {
			t.Errorf("%s: code %s, want NAME_UNKNOWN", ref, got)
		}
	}
	if got := m.Stats().BytesStored; got != 0 {
		t.Errorf("%d bytes kept", got)
	}
}

func TestAManifestUpstreamLimitsAreRelayedAsFallbackableErrors(t *testing.T) {
	f := newFakeRegistry(t)
	limited := f.addManifest("library/alpine", "", manifestBody("l"), dockerV2)
	broken := f.addManifest("library/alpine", "", manifestBody("b"), dockerV2)
	f.override["/v2/library/alpine/manifests/"+limited] = http.StatusTooManyRequests
	f.override["/v2/library/alpine/manifests/"+broken] = http.StatusInternalServerError
	_, srv := newTestMirror(t, f, nil)

	for digest, want := range map[string]int{limited: http.StatusTooManyRequests, broken: http.StatusBadGateway} {
		resp, _ := do(t, http.MethodGet, srv.URL+"/v2/library/alpine/manifests/"+digest, nil)
		if resp.StatusCode != want {
			t.Errorf("status %d, want %d", resp.StatusCode, want)
		}
	}
}

// A mirror that cannot tell what a tag points at cannot serve it, and says so
// in a way dockerd falls back from rather than guessing.
func TestATagTheRegistryGivesNoDigestForIsAnswered502(t *testing.T) {
	f := newFakeRegistry(t)
	f.addManifest("library/alpine", "latest", manifestBody("a"), dockerV2)
	f.omitDigest = true
	_, srv := newTestMirror(t, f, nil)

	resp, _ := do(t, http.MethodGet, srv.URL+"/v2/library/alpine/manifests/latest", accept(dockerV2))

	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status %d, want 502", resp.StatusCode)
	}
}

func TestAHeadForAManifestByDigestIsAnsweredWithItsHeadersAndNoBody(t *testing.T) {
	f := newFakeRegistry(t)
	body := manifestBody("h")
	digest := f.addManifest("library/alpine", "", body, ociIndex)
	_, srv := newTestMirror(t, f, nil)

	resp, got := do(t, http.MethodHead, srv.URL+"/v2/library/alpine/manifests/"+digest, accept(dockerV2))

	if resp.StatusCode != http.StatusOK || len(got) != 0 {
		t.Fatalf("status %d with %d body bytes, want 200 and none", resp.StatusCode, len(got))
	}
	if ct := resp.Header.Get("Content-Type"); ct != ociIndex {
		t.Errorf("Content-Type %q, want %q", ct, ociIndex)
	}
	if cl := resp.Header.Get("Content-Length"); cl != fmt.Sprint(len(body)) {
		t.Errorf("Content-Length %q, want %d", cl, len(body))
	}
}

// What the mirror remembers about tags is held in memory, and a job chooses the
// tags, so there is a bound, and the oldest answers go first.
func TestWhatIsRememberedAboutTagsIsBounded(t *testing.T) {
	f := newFakeRegistry(t)
	for i := 0; i < 8; i++ {
		f.addManifest("library/alpine", fmt.Sprintf("t%d", i), manifestBody(fmt.Sprint(i)), dockerV2)
	}
	m, srv := newTestMirror(t, f, nil)
	m.maxTags = 3

	for i := 0; i < 8; i++ {
		do(t, http.MethodHead, srv.URL+fmt.Sprintf("/v2/library/alpine/manifests/t%d", i), nil)
	}

	if n := f.count("manifest HEAD"); n != 8 {
		t.Fatalf("upstream saw %d HEADs for 8 distinct tags, want 8", n)
	}
	if n := m.tagCount(); n == 0 || n > 3 {
		t.Errorf("%d tags remembered, want between 1 and 3", n)
	}
	do(t, http.MethodHead, srv.URL+"/v2/library/alpine/manifests/t7", nil)
	if n := f.count("manifest HEAD"); n != 8 {
		t.Errorf("the newest tag was asked about again: %d HEADs, want 8", n)
	}
}
