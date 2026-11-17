package registrymirror

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func blobOf(n int, fill byte) []byte { return bytes.Repeat([]byte{fill}, n) }

func TestABlobIsFetchedOnceAndThenServedFromTheMirrorsOwnDisk(t *testing.T) {
	f := newFakeRegistry(t)
	data := blobOf(100_000, 'a')
	digest := f.addBlob(data)
	m, srv := newTestMirror(t, f, nil)
	url := srv.URL + "/v2/library/alpine/blobs/" + digest

	first, body1 := do(t, http.MethodGet, url, nil)
	second, body2 := do(t, http.MethodGet, url, nil)

	for i, r := range []*http.Response{first, second} {
		if r.StatusCode != http.StatusOK {
			t.Fatalf("request %d: status %d", i+1, r.StatusCode)
		}
		if got := r.Header.Get("Docker-Content-Digest"); got != digest {
			t.Errorf("request %d: Docker-Content-Digest %q, want %q", i+1, got, digest)
		}
		if got := r.Header.Get("Content-Length"); got != fmt.Sprint(len(data)) {
			t.Errorf("request %d: Content-Length %q, want %d", i+1, got, len(data))
		}
	}
	if !bytes.Equal(body1, data) || !bytes.Equal(body2, data) {
		t.Fatal("a body did not match what upstream holds")
	}
	if n := f.count("blob"); n != 1 {
		t.Errorf("upstream was asked for the blob %d times, want 1", n)
	}
	if n := f.count("cdn"); n != 1 {
		t.Errorf("the content network was asked %d times, want 1", n)
	}
	got := m.Stats()
	want := Stats{Requests: 2, Hits: 1, Misses: 1, BytesFetched: int64(len(data)), BytesServed: 2 * int64(len(data)), BytesStored: int64(len(data))}
	if got != want {
		t.Errorf("stats %+v, want %+v", got, want)
	}
}

// The registry's token is for the registry. Handing it to whatever host the
// registry redirects a download to would hand it to a third party, and the
// mirror holds no other credential to hand over.
func TestAMirrorFollowsABlobRedirectWithoutSendingItTheRegistrysToken(t *testing.T) {
	f := newFakeRegistry(t)
	digest := f.addBlob(blobOf(1000, 'b'))
	_, srv := newTestMirror(t, f, nil)

	do(t, http.MethodGet, srv.URL+"/v2/library/alpine/blobs/"+digest, nil)

	if f.count("cdn") != 1 {
		t.Fatalf("the content network was asked %d times, want 1", f.count("cdn"))
	}
	for _, a := range f.cdnAuth {
		if a != "" {
			t.Errorf("the content network was sent Authorization %q", a)
		}
	}
	for _, a := range f.tokenAuth {
		if a != "" {
			t.Errorf("the token service was sent Authorization %q: upstream is anonymous", a)
		}
	}
}

// What is kept is kept under its digest, so bytes that do not hash to the
// digest they were asked for must never be, or the next job would be served
// them from disk. A blob of known length is checked before its last chunk is
// sent, so the client is never handed a corrupt blob whole: it is cut short, and
// dockerd treats that as a failure to fall back from.
func TestABlobWhoseBytesDoNotMatchItsDigestIsNeverKeptOrDeliveredWhole(t *testing.T) {
	f := newFakeRegistry(t)
	digest := f.addBlob(blobOf(5000, 'c'))
	f.badBlob[digest] = blobOf(5000, 'X')
	m, srv := newTestMirror(t, f, nil)
	url := srv.URL + "/v2/library/alpine/blobs/" + digest

	body, _, err := fetch(url, nil)
	if err == nil {
		t.Errorf("the client was given a corrupt blob whole: %d bytes and no error", len(body))
	}
	fetch(url, nil)

	if got := m.Stats().BytesStored; got != 0 {
		t.Errorf("%d bytes were kept", got)
	}
	if n := f.count("cdn"); n != 2 {
		t.Errorf("the content network was asked %d times, want 2: the second request must not be served from disk", n)
	}
}

// Upstream is anonymous, so a private repository is refused there and there is
// nothing to keep. That is the whole of "never caches a private image": it is
// a property of the request, not a rule the mirror has to remember. The answer
// is a 404, which is what Docker Hub's 401 means to an anonymous caller (private
// and absent look the same), and never a 401, which would send dockerd off to
// authenticate against the mirror.
func TestAPrivateRepositoryIsNeverKept(t *testing.T) {
	f := newFakeRegistry(t)
	digest := f.addBlob(blobOf(2000, 'd'))
	f.private["acme/secret"] = true
	m, srv := newTestMirror(t, f, nil)

	resp, body := do(t, http.MethodGet, srv.URL+"/v2/acme/secret/blobs/"+digest, nil)

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status %d, want 404", resp.StatusCode)
	}
	if got := registryErrorCode(t, body); got != "NAME_UNKNOWN" {
		t.Errorf("code %s, want NAME_UNKNOWN", got)
	}
	if resp.Header.Get("WWW-Authenticate") != "" {
		t.Error("the answer carried an authentication challenge")
	}
	if got := m.Stats().BytesStored; got != 0 {
		t.Errorf("%d bytes were kept", got)
	}
	if n := f.count("cdn"); n != 0 {
		t.Errorf("the content network was asked %d times for a private repository", n)
	}
}

// Every failure has to leave dockerd something it falls back from. A rate limit
// is relayed as one, because that is what it is; anything else upstream does
// wrong is a 502; and a blob upstream does not have is a 404.
func TestAnUpstreamFailureIsAnsweredWithSomethingDockerFallsBackFrom(t *testing.T) {
	f := newFakeRegistry(t)
	present := f.addBlob(blobOf(100, 'e'))
	absent := digestOf([]byte("not published"))
	limited := f.addBlob(blobOf(100, 'f'))
	broken := f.addBlob(blobOf(100, 'g'))
	f.override["/v2/library/alpine/blobs/"+limited] = http.StatusTooManyRequests
	f.override["/v2/library/alpine/blobs/"+broken] = http.StatusInternalServerError
	_, srv := newTestMirror(t, f, nil)
	_ = present

	cases := []struct {
		name   string
		digest string
		status int
		code   string
	}{
		{"a blob upstream does not have", absent, http.StatusNotFound, "BLOB_UNKNOWN"},
		{"a rate limit", limited, http.StatusTooManyRequests, "TOOMANYREQUESTS"},
		{"a server error", broken, http.StatusBadGateway, "UNAVAILABLE"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp, body := do(t, http.MethodGet, srv.URL+"/v2/library/alpine/blobs/"+c.digest, nil)

			if resp.StatusCode != c.status {
				t.Fatalf("status %d, want %d", resp.StatusCode, c.status)
			}
			if got := registryErrorCode(t, body); got != c.code {
				t.Errorf("code %s, want %s", got, c.code)
			}
			// How long to wait is the registry's to say, and a client that
			// retries needs to hear it.
			if want := map[int]string{http.StatusTooManyRequests: "30"}[c.status]; resp.Header.Get("Retry-After") != want {
				t.Errorf("Retry-After %q, want %q", resp.Header.Get("Retry-After"), want)
			}
		})
	}
}

func TestAnUpstreamThatCannotBeReachedIsAnswered502(t *testing.T) {
	f := newFakeRegistry(t)
	digest := f.addBlob(blobOf(100, 'h'))
	_, srv := newTestMirror(t, f, nil)
	f.reg.Close()

	resp, body := do(t, http.MethodGet, srv.URL+"/v2/library/alpine/blobs/"+digest, nil)

	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status %d, want 502", resp.StatusCode)
	}
	if got := registryErrorCode(t, body); got != "UNAVAILABLE" {
		t.Errorf("code %s, want UNAVAILABLE", got)
	}
}

// A token is for one repository and lasts a while. Asking for one on every
// request would double the load on the token service for nothing.
func TestATokenIsReusedForARepositoryWhileItLasts(t *testing.T) {
	f := newFakeRegistry(t)
	one := f.addBlob(blobOf(100, '1'))
	two := f.addBlob(blobOf(100, '2'))
	three := f.addBlob(blobOf(100, '3'))
	_, srv := newTestMirror(t, f, nil)

	do(t, http.MethodGet, srv.URL+"/v2/library/alpine/blobs/"+one, nil)
	do(t, http.MethodGet, srv.URL+"/v2/library/alpine/blobs/"+two, nil)
	if n := f.count("token"); n != 1 {
		t.Errorf("%d tokens for one repository, want 1", n)
	}

	// A blob the mirror already holds is served without asking upstream, and so
	// without a token, whichever repository it is asked for under; this one is new.
	do(t, http.MethodGet, srv.URL+"/v2/library/ubuntu/blobs/"+three, nil)
	if n := f.count("token"); n != 2 {
		t.Errorf("%d tokens after a second repository, want 2", n)
	}
}

// A HEAD asks what is there without asking for the bytes. One for a blob the
// mirror holds is answered from its own disk, and one for a blob it does not
// hold is not turned into a download.
func TestAHeadForABlobNeverFetchesItsBytes(t *testing.T) {
	f := newFakeRegistry(t)
	data := blobOf(3000, 'i')
	digest := f.addBlob(data)
	m, srv := newTestMirror(t, f, nil)
	url := srv.URL + "/v2/library/alpine/blobs/" + digest

	resp, body := do(t, http.MethodHead, url, nil)

	if resp.StatusCode != http.StatusOK || len(body) != 0 {
		t.Fatalf("status %d with %d body bytes, want 200 and none", resp.StatusCode, len(body))
	}
	if got := resp.Header.Get("Content-Length"); got != fmt.Sprint(len(data)) {
		t.Errorf("Content-Length %q, want %d", got, len(data))
	}
	if m.Stats().BytesStored != 0 {
		t.Error("a HEAD kept bytes")
	}

	do(t, http.MethodGet, url, nil)
	resp, body = do(t, http.MethodHead, url, nil)

	if resp.StatusCode != http.StatusOK || len(body) != 0 {
		t.Fatalf("status %d with %d body bytes after the blob was kept, want 200 and none", resp.StatusCode, len(body))
	}
	if !strings.HasPrefix(resp.Header.Get("Docker-Content-Digest"), "sha256:") {
		t.Error("no Docker-Content-Digest on a HEAD")
	}
}
