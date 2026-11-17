package registrymirror

import (
	"net/http"
	"strings"
	"testing"
)

// dockerd opens every pull with GET /v2/ and sends no credential. A challenge
// here would send it off to authenticate against a mirror that has nothing to
// authenticate, so the ping is a bare 200.
func TestAMirrorAnswersThePingWithoutAChallenge(t *testing.T) {
	f := newFakeRegistry(t)
	_, srv := newTestMirror(t, f, nil)

	resp, _ := do(t, http.MethodGet, srv.URL+"/v2/", nil)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200", resp.StatusCode)
	}
	if v := resp.Header.Get("WWW-Authenticate"); v != "" {
		t.Errorf("challenged with %q", v)
	}
	if v := resp.Header.Get("Docker-Distribution-API-Version"); v != "registry/2.0" {
		t.Errorf("API version header %q, want registry/2.0", v)
	}
	if n := f.total(); n != 0 {
		t.Errorf("the ping reached upstream %d times", n)
	}
}

// Nothing a job does through the mirror may change what is upstream or what the
// mirror holds, so everything but a read is refused, and refused before any
// request is made upstream.
func TestAMirrorRefusesEveryMethodButGetAndHead(t *testing.T) {
	digest := digestOf([]byte("x"))
	paths := []string{
		"/v2/library/alpine/manifests/latest",
		"/v2/library/alpine/blobs/" + digest,
		"/v2/library/alpine/blobs/uploads/",
	}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		for _, path := range paths {
			t.Run(method+" "+path, func(t *testing.T) {
				f := newFakeRegistry(t)
				_, srv := newTestMirror(t, f, nil)

				resp, body := do(t, method, srv.URL+path, nil)

				if resp.StatusCode != http.StatusMethodNotAllowed {
					t.Fatalf("status %d, want 405", resp.StatusCode)
				}
				if allow := resp.Header.Get("Allow"); allow != "GET, HEAD" {
					t.Errorf("Allow %q, want %q", allow, "GET, HEAD")
				}
				_ = registryErrorCode(t, body)
				if n := f.total(); n != 0 {
					t.Errorf("%d requests reached upstream", n)
				}
			})
		}
	}
}

// A job chooses the path, and the mirror makes the outbound request for it, so
// a name or a reference outside the registry's own grammar never gets that far.
func TestAMirrorRefusesAnInvalidNameOrReferenceBeforeAskingUpstream(t *testing.T) {
	good := digestOf([]byte("x"))
	cases := []struct {
		path string
		code string
	}{
		{"/v2/Library/alpine/manifests/latest", "NAME_INVALID"},
		{"/v2/library/../etc/manifests/latest", "NAME_INVALID"},
		{"/v2/library//alpine/manifests/latest", "NAME_INVALID"},
		{"/v2/-library/alpine/manifests/latest", "NAME_INVALID"},
		{"/v2/library/alpine-/manifests/latest", "NAME_INVALID"},
		{"/v2/library%2f..%2falpine/manifests/latest", "NAME_INVALID"},
		{"/v2/" + strings.Repeat("a", 256) + "/manifests/latest", "NAME_INVALID"},
		{"/v2/library/alpine/manifests/bad%20tag", "TAG_INVALID"},
		{"/v2/library/alpine/manifests/-leading", "TAG_INVALID"},
		{"/v2/library/alpine/manifests/" + strings.Repeat("t", 129), "TAG_INVALID"},
		{"/v2/library/alpine/manifests/sha256:short", "DIGEST_INVALID"},
		{"/v2/library/alpine/manifests/sha512:" + strings.Repeat("a", 128), "DIGEST_INVALID"},
		{"/v2/library/alpine/blobs/latest", "DIGEST_INVALID"},
		{"/v2/library/alpine/blobs/sha256:" + strings.Repeat("G", 64), "DIGEST_INVALID"},
		{"/v2/Library/alpine/blobs/" + good, "NAME_INVALID"},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			f := newFakeRegistry(t)
			_, srv := newTestMirror(t, f, nil)

			resp, body := do(t, http.MethodGet, srv.URL+c.path, nil)

			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status %d, want 400", resp.StatusCode)
			}
			if got := registryErrorCode(t, body); got != c.code {
				t.Errorf("code %s, want %s", got, c.code)
			}
			if n := f.total(); n != 0 {
				t.Errorf("%d requests reached upstream", n)
			}
		})
	}
}

// A registry has routes the mirror does not serve: listing tags, the catalogue.
// They are answered as unsupported, never forwarded.
func TestAMirrorAnswersARouteItDoesNotServeAsUnsupported(t *testing.T) {
	for _, path := range []string{"/v2/library/alpine/tags/list", "/v2/_catalog", "/", "/v2/library/alpine"} {
		t.Run(path, func(t *testing.T) {
			f := newFakeRegistry(t)
			_, srv := newTestMirror(t, f, nil)

			resp, body := do(t, http.MethodGet, srv.URL+path, nil)

			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("status %d, want 404", resp.StatusCode)
			}
			if got := registryErrorCode(t, body); got != "UNSUPPORTED" {
				t.Errorf("code %s, want UNSUPPORTED", got)
			}
			if n := f.total(); n != 0 {
				t.Errorf("%d requests reached upstream", n)
			}
		})
	}
}
