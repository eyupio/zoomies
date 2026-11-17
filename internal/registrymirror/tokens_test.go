package registrymirror

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

// A token is kept for the repository it was issued for, and the repository is
// named by whoever sends the request. A registry issues one even for a
// repository that does not exist, so the bounded tag cache and the bounded store
// do not limit this one: without a bound of its own, a long-running mirror
// asked about enough distinct names would grow without limit.
func TestTheTokenCacheIsBounded(t *testing.T) {
	f := newFakeRegistry(t)
	digest := digestOf([]byte("not published"))
	m, srv := newTestMirror(t, f, nil)
	m.tokens.maxEntries = 5

	for i := 0; i < 12; i++ {
		do(t, http.MethodGet, srv.URL+fmt.Sprintf("/v2/lib/r%d/blobs/%s", i, digest), nil)
	}

	if n := f.count("token"); n != 12 {
		t.Fatalf("%d token exchanges for 12 distinct repositories, want 12", n)
	}
	if n := m.tokens.count(); n == 0 || n > 5 {
		t.Errorf("%d tokens held, want between 1 and 5", n)
	}
}

// What has expired is of no use, so it is the first to go when room is needed,
// and a mirror that has been quiet for a while does not hold on to it.
func TestExpiredTokensAreDroppedWhenANewOneIsAdded(t *testing.T) {
	f := newFakeRegistry(t)
	clock := newFakeClock()
	digest := digestOf([]byte("not published"))
	m, srv := newTestMirror(t, f, func(c *Config) { c.Now = clock.Now })
	for i := 0; i < 3; i++ {
		do(t, http.MethodGet, srv.URL+fmt.Sprintf("/v2/lib/old%d/blobs/%s", i, digest), nil)
	}
	if n := m.tokens.count(); n != 3 {
		t.Fatalf("%d tokens held after three repositories, want 3", n)
	}

	clock.advance(time.Hour)
	do(t, http.MethodGet, srv.URL+"/v2/lib/new/blobs/"+digest, nil)

	if n := m.tokens.count(); n != 1 {
		t.Errorf("%d tokens held after an hour and one new repository, want 1: the three that expired are gone", n)
	}
}
