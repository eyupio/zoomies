package registrymirror

import (
	"net/http"
	"testing"
	"time"
)

// A mirror that cannot get an answer from the registry is slower than no mirror,
// because every pull waits for it to fail before the daemon falls back. A single
// failure is a blip and an hour of them is a fault, so the mirror says how long
// the registry has been failing it without a break, and the agent decides when
// that is long enough to tell an operator.

func failingManifest(f *fakeRegistry, status int) (url string) {
	digest := f.addManifest("library/alpine", "", manifestBody("health"), ociIndex)
	f.override["/v2/library/alpine/manifests/"+digest] = status
	return "/v2/library/alpine/manifests/" + digest
}

func TestAnUnbrokenRunOfUpstreamFailuresIsTimedFromItsFirst(t *testing.T) {
	f := newFakeRegistry(t)
	clock := newFakeClock()
	path := failingManifest(f, http.StatusInternalServerError)
	m, srv := newTestMirror(t, f, func(c *Config) { c.Now = clock.Now })

	if got := m.Stats().UpstreamFailingSeconds; got != 0 {
		t.Fatalf("a mirror that has asked for nothing has been failing for %d seconds", got)
	}
	do(t, http.MethodGet, srv.URL+path, nil)
	clock.advance(2 * time.Minute)
	do(t, http.MethodGet, srv.URL+path, nil)
	clock.advance(3 * time.Minute)
	// Five minutes since the first failure, not three since the latest: it is
	// how long nothing has worked that says a registry is down.
	if got := m.Stats().UpstreamFailingSeconds; got != 300 {
		t.Fatalf("failing for %d seconds, want 300 (the run began at its first failure)", got)
	}
}

func TestAnAnswerFromTheRegistryEndsTheRun(t *testing.T) {
	f := newFakeRegistry(t)
	clock := newFakeClock()
	broken := failingManifest(f, http.StatusBadGateway)
	good := f.addManifest("library/alpine", "", manifestBody("good"), ociIndex)
	m, srv := newTestMirror(t, f, func(c *Config) { c.Now = clock.Now })

	do(t, http.MethodGet, srv.URL+broken, nil)
	clock.advance(time.Minute)
	if got := m.Stats().UpstreamFailingSeconds; got != 60 {
		t.Fatalf("failing for %d seconds, want 60", got)
	}
	do(t, http.MethodGet, srv.URL+"/v2/library/alpine/manifests/"+good, nil)
	if got := m.Stats().UpstreamFailingSeconds; got != 0 {
		t.Fatalf("failing for %d seconds after the registry answered", got)
	}
	// A later failure starts a run of its own and does not inherit the old one.
	clock.advance(10 * time.Minute)
	do(t, http.MethodGet, srv.URL+broken, nil)
	clock.advance(time.Minute)
	if got := m.Stats().UpstreamFailingSeconds; got != 60 {
		t.Fatalf("failing for %d seconds, want 60: the earlier run ended", got)
	}
}

// A registry that says no has answered. A private repository, an absent image
// and a tag that does not exist are the registry doing its job, and a mirror
// that counted them would call itself broken for serving a job that named an
// image wrongly.
func TestARegistryThatSaysNoIsReachable(t *testing.T) {
	f := newFakeRegistry(t)
	clock := newFakeClock()
	f.private["library/secret"] = true
	m, srv := newTestMirror(t, f, func(c *Config) { c.Now = clock.Now })
	for _, path := range []string{
		"/v2/library/alpine/manifests/latest",
		"/v2/library/secret/manifests/latest",
		"/v2/library/alpine/blobs/" + digestOf([]byte("absent")),
	} {
		do(t, http.MethodGet, srv.URL+path, nil)
	}
	clock.advance(time.Hour)
	if got := m.Stats().UpstreamFailingSeconds; got != 0 {
		t.Fatalf("failing for %d seconds when the registry only said no", got)
	}
}

func TestARegistryThatLimitsTheMirrorIsFailingIt(t *testing.T) {
	f := newFakeRegistry(t)
	clock := newFakeClock()
	path := failingManifest(f, http.StatusTooManyRequests)
	m, srv := newTestMirror(t, f, func(c *Config) { c.Now = clock.Now })
	do(t, http.MethodGet, srv.URL+path, nil)
	clock.advance(time.Minute)
	if got := m.Stats().UpstreamFailingSeconds; got != 60 {
		t.Fatalf("failing for %d seconds, want 60: a rate limit is a mirror that cannot fetch", got)
	}
}

// The registry that answers its headers and then stops sending is the one a
// check on the first byte would never catch, and the mirror is as useless
// against it as against one that is down.
func TestABlobThatStallsPartwayBeginsARun(t *testing.T) {
	f := newFakeRegistry(t)
	clock := newFakeClock()
	digest := f.addBlob(blobOf(500, 's'))
	f.setStall(true)
	m, srv := newTestMirror(t, f, func(c *Config) {
		c.Now = clock.Now
		c.IdleTimeout = 150 * time.Millisecond
	})
	fetch(srv.URL+"/v2/library/alpine/blobs/"+digest, nil)
	clock.advance(time.Minute)
	if got := m.Stats().UpstreamFailingSeconds; got != 60 {
		t.Fatalf("failing for %d seconds, want 60: a transfer that stopped is a failure", got)
	}
}

func TestABlobServedWholeEndsTheRun(t *testing.T) {
	f := newFakeRegistry(t)
	clock := newFakeClock()
	broken := failingManifest(f, http.StatusInternalServerError)
	digest := f.addBlob(blobOf(500, 'w'))
	m, srv := newTestMirror(t, f, func(c *Config) { c.Now = clock.Now })
	do(t, http.MethodGet, srv.URL+broken, nil)
	clock.advance(time.Minute)
	do(t, http.MethodGet, srv.URL+"/v2/library/alpine/blobs/"+digest, nil)
	if got := m.Stats().UpstreamFailingSeconds; got != 0 {
		t.Fatalf("failing for %d seconds after a whole blob arrived", got)
	}
}

func TestAManifestFetchedByTagEndsTheRun(t *testing.T) {
	f := newFakeRegistry(t)
	clock := newFakeClock()
	broken := failingManifest(f, http.StatusInternalServerError)
	f.addManifest("library/busybox", "latest", manifestBody("tagged"), ociIndex)
	m, srv := newTestMirror(t, f, func(c *Config) { c.Now = clock.Now })
	do(t, http.MethodGet, srv.URL+broken, nil)
	clock.advance(time.Minute)
	do(t, http.MethodGet, srv.URL+"/v2/library/busybox/manifests/latest", nil)
	if got := m.Stats().UpstreamFailingSeconds; got != 0 {
		t.Fatalf("failing for %d seconds after a tag was resolved", got)
	}
}

func TestABlobsHeadRequestEndsTheRun(t *testing.T) {
	f := newFakeRegistry(t)
	clock := newFakeClock()
	broken := failingManifest(f, http.StatusInternalServerError)
	digest := f.addBlob(blobOf(500, 'h'))
	m, srv := newTestMirror(t, f, func(c *Config) { c.Now = clock.Now })
	do(t, http.MethodGet, srv.URL+broken, nil)
	clock.advance(time.Minute)
	do(t, http.MethodHead, srv.URL+"/v2/library/alpine/blobs/"+digest, nil)
	if got := m.Stats().UpstreamFailingSeconds; got != 0 {
		t.Fatalf("failing for %d seconds after the registry answered a HEAD", got)
	}
}
