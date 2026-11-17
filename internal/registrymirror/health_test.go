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

// Each way a blob can arrive whole is an answer: the registry gave what it was
// asked for. They are checked separately because each is a different line, and a
// mirror that ended a run for some and not others would call itself broken after
// serving a perfectly good image.
func TestABlobServedWholeEndsTheRun(t *testing.T) {
	for _, tc := range []struct {
		name    string
		size    int
		chunked bool
		max     int64
		ranged  bool
	}{
		{"of known length", 500, false, 0, false},
		{"of unknown length", 500, true, 0, false},
		{"too large to keep, of known length", 5000, false, 4000, false},
		{"too large to keep, of unknown length", 5000, true, 4000, false},
		{"as a part of it", 500, false, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeRegistry(t)
			f.cdnChunked = tc.chunked
			clock := newFakeClock()
			broken := failingManifest(f, http.StatusInternalServerError)
			digest := f.addBlob(blobOf(tc.size, 'w'))
			m, srv := newTestMirror(t, f, func(c *Config) {
				c.Now = clock.Now
				if tc.max > 0 {
					c.MaxBytes = tc.max
				}
			})
			do(t, http.MethodGet, srv.URL+broken, nil)
			clock.advance(time.Minute)
			header := http.Header(nil)
			if tc.ranged {
				header = http.Header{"Range": {"bytes=0-99"}}
			}
			do(t, http.MethodGet, srv.URL+"/v2/library/alpine/blobs/"+digest, header)
			if got := m.Stats().UpstreamFailingSeconds; got != 0 {
				t.Fatalf("failing for %d seconds after a whole blob arrived", got)
			}
		})
	}
}

// A HEAD of a tag is answered by the registry alone, with no body to read, so
// it is the one request that ends the run by itself. A GET would go on to fetch
// the manifest, whose body ends it anyway, and would not show this.
func TestAHeadOfATagEndsTheRun(t *testing.T) {
	f := newFakeRegistry(t)
	clock := newFakeClock()
	broken := failingManifest(f, http.StatusInternalServerError)
	f.addManifest("library/busybox", "latest", manifestBody("tagged"), ociIndex)
	m, srv := newTestMirror(t, f, func(c *Config) { c.Now = clock.Now })
	do(t, http.MethodGet, srv.URL+broken, nil)
	clock.advance(time.Minute)
	do(t, http.MethodHead, srv.URL+"/v2/library/busybox/manifests/latest", nil)
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

// A response that arrives whole and is wrong is the registry failing the mirror
// as much as one that never arrived, so it must not be what ends a run of
// failures. The body reaching its end is not the registry having answered: that
// is a body that is whole and is what was asked for.

func TestACorruptManifestDoesNotEndARunOfFailures(t *testing.T) {
	f := newFakeRegistry(t)
	clock := newFakeClock()
	broken := failingManifest(f, http.StatusInternalServerError)
	claimed := digestOf([]byte("what the manifest claims to be"))
	f.addManifestUnder("library/alpine", claimed, manifestBody("forged"), ociIndex)
	m, srv := newTestMirror(t, f, func(c *Config) { c.Now = clock.Now })

	do(t, http.MethodGet, srv.URL+broken, nil)
	clock.advance(4 * time.Minute)
	do(t, http.MethodGet, srv.URL+"/v2/library/alpine/manifests/"+claimed, nil)
	clock.advance(2 * time.Minute)
	// Six minutes since the first failure: the forged manifest in the middle was
	// a failure too, and did not start the run again from where it arrived.
	if got := m.Stats().UpstreamFailingSeconds; got != 360 {
		t.Fatalf("failing for %d seconds, want 360: a manifest that does not hash to its digest ended the run", got)
	}
}

func TestACorruptBlobDoesNotEndARunOfFailures(t *testing.T) {
	for name, chunked := range map[string]bool{
		"of known length":   false,
		"of unknown length": true,
	} {
		t.Run(name, func(t *testing.T) {
			f := newFakeRegistry(t)
			f.cdnChunked = chunked
			clock := newFakeClock()
			broken := failingManifest(f, http.StatusInternalServerError)
			digest := f.addBlob(blobOf(500, 'g'))
			f.badBlob[digest] = blobOf(500, 'X')
			m, srv := newTestMirror(t, f, func(c *Config) { c.Now = clock.Now })

			do(t, http.MethodGet, srv.URL+broken, nil)
			errorsBefore := m.Stats().UpstreamErrors
			clock.advance(4 * time.Minute)
			fetch(srv.URL+"/v2/library/alpine/blobs/"+digest, nil)
			clock.advance(2 * time.Minute)
			if got := m.Stats().UpstreamFailingSeconds; got != 360 {
				t.Fatalf("failing for %d seconds, want 360: a blob that does not hash to its digest ended the run", got)
			}
			// At least one, not exactly one: a blob of known length that is small
			// enough to be one chunk is cut off before the client has been sent a
			// byte, and Go's HTTP client then retries a GET once, so the mirror is
			// asked twice and counts it twice.
			if got := m.Stats().UpstreamErrors; got <= errorsBefore {
				t.Fatalf("%d upstream errors, want more than %d: a corrupt blob is one", got, errorsBefore)
			}
		})
	}
}
