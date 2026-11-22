package registrymirror

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Whoever asks for a blob first is sent it as it arrives, and everyone who asks
// while that is happening waits for the download to be done and is then served from
// disk. A client that stopped reading but kept its connection open left the mirror
// blocked in a write that nothing could end: the idle timeout cancels a stalled
// download, and cancelling does not unblock a write. So the download stopped where
// it was, the flight was never ended, and every other request for that blob waited
// behind a client that was not reading, with nothing to say why.
//
// The stalled client is given up on, as one that has gone away is, and the download
// goes on for the blob's keeper and everyone waiting for it.
func TestAClientThatStopsReadingDoesNotHoldTheBlobFromEveryoneElse(t *testing.T) {
	f := newFakeRegistry(t)
	data := blobOf(64<<20, 'q')
	digest := f.addBlob(data)
	m, srv := newTestMirror(t, f, func(c *Config) {
		c.MaxBytes = 512 << 20
		c.IdleTimeout = 600 * time.Millisecond
	})
	path := "/v2/library/alpine/blobs/" + digest

	// A client that asks and never reads: its end of the connection fills, then
	// the mirror's write blocks.
	conn, err := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.SetReadBuffer(1024)
	}
	if _, err := fmt.Fprintf(conn, "GET %s HTTP/1.1\r\nHost: mirror\r\n\r\n", path); err != nil {
		t.Fatal(err)
	}
	// Long enough for the download to be under way and the write to have blocked.
	time.Sleep(200 * time.Millisecond)

	started := time.Now()
	resp, body := do(t, http.MethodGet, srv.URL+path, nil)
	if resp.StatusCode != http.StatusOK || !bytes.Equal(body, data) {
		t.Fatalf("the second client got status %d and %d bytes, want the whole blob", resp.StatusCode, len(body))
	}
	if waited := time.Since(started); waited > 8*time.Second {
		t.Errorf("the second client waited %s behind one that was not reading", waited)
	}
	if _, ok := m.store.open(digest); !ok {
		t.Error("the blob was not kept, so the next job is owed a download the mirror had already made")
	}
}

// The deadline on a write to a client is for a client that is not reading. Over
// HTTP/2 Go resets the stream when it expires, whether or not the write it was set
// for has long been done, so one left armed through a pause upstream, between half
// the idle timeout and the whole of it, cut off a download whose client had read
// every byte it was sent. It has to be taken down as soon as the write is over.
func TestAPauseUpstreamShorterThanTheIdleTimeoutDoesNotCutAnHTTP2Download(t *testing.T) {
	f := newFakeRegistry(t)
	data := blobOf(2<<20, 'h')
	digest := f.addBlob(data)
	f.mu.Lock()
	f.cdnPause = 750 * time.Millisecond
	f.mu.Unlock()
	m, _ := newTestMirror(t, f, func(c *Config) { c.IdleTimeout = time.Second })

	srv := httptest.NewUnstartedServer(m)
	srv.EnableHTTP2 = true
	srv.StartTLS()
	t.Cleanup(srv.Close)

	resp, err := srv.Client().Get(srv.URL + "/v2/library/alpine/blobs/" + digest)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.ProtoMajor != 2 {
		t.Fatalf("the test spoke HTTP/%d, so it proves nothing about HTTP/2", resp.ProtoMajor)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading the blob: %v (after %d of %d bytes)", err, len(body), len(data))
	}
	if !bytes.Equal(body, data) {
		t.Fatalf("got %d bytes, want the whole %d", len(body), len(data))
	}
}
