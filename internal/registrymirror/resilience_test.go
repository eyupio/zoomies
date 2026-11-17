package registrymirror

import (
	"bytes"
	"context"
	"net/http"
	"sync"
	"testing"
	"time"
)

// Eight jobs starting on one host pull the same base image at once. The mirror
// exists to make that one request upstream, not eight.
func TestConcurrentRequestsForOneBlobShareOneFetch(t *testing.T) {
	f := newFakeRegistry(t)
	f.cdnDelay = 300 * time.Millisecond
	data := blobOf(50_000, 'z')
	digest := f.addBlob(data)
	m, srv := newTestMirror(t, f, nil)
	url := srv.URL + "/v2/library/alpine/blobs/" + digest

	const clients = 8
	bodies := make([][]byte, clients)
	errs := make([]error, clients)
	var wg sync.WaitGroup
	for i := 0; i < clients; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bodies[i], _, errs[i] = fetch(url, nil)
		}()
	}
	wg.Wait()

	for i := range bodies {
		if errs[i] != nil || !bytes.Equal(bodies[i], data) {
			t.Fatalf("client %d: error %v, %d bytes", i, errs[i], len(bodies[i]))
		}
	}
	if n := f.count("cdn"); n != 1 {
		t.Errorf("the content network was asked %d times for %d clients, want 1", n, clients)
	}
	if got := m.Stats(); got.Misses != 1 || got.Hits != clients-1 {
		t.Errorf("%d misses and %d hits, want 1 and %d", got.Misses, got.Hits, clients-1)
	}
}

// A registry that stops sending halfway is given up on after the idle timeout,
// and the client is told the blob did not arrive rather than that it is whole.
// Nothing is kept, and a later request works once the registry does.
func TestAnUpstreamThatStallsMidBlobIsGivenUpOn(t *testing.T) {
	f := newFakeRegistry(t)
	data := blobOf(4000, 's')
	digest := f.addBlob(data)
	f.setStall(true)
	m, srv := newTestMirror(t, f, func(c *Config) { c.IdleTimeout = 150 * time.Millisecond })
	url := srv.URL + "/v2/library/alpine/blobs/" + digest

	start := time.Now()
	body, _, err := fetch(url, nil)

	if err == nil {
		t.Errorf("the client was told a blob of %d bytes arrived whole", len(body))
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("giving up took %v, want about the 150ms idle timeout", d)
	}
	if got := m.Stats(); got.BytesStored != 0 || got.UpstreamErrors == 0 {
		t.Errorf("%d bytes kept and %d upstream errors, want none kept and at least one error", got.BytesStored, got.UpstreamErrors)
	}

	f.setStall(false)
	body, status, err := fetch(url, nil)
	if err != nil || status != http.StatusOK || !bytes.Equal(body, data) {
		t.Errorf("after the registry recovered: status %d, error %v, %d bytes", status, err, len(body))
	}
}

// Whoever asked first may give up, and the next job is still owed the blob it
// would have been served, so a download that will be kept is finished.
func TestABlobIsStillKeptWhenTheClientThatAskedForItGoesAway(t *testing.T) {
	f := newFakeRegistry(t)
	f.cdnDelay = 300 * time.Millisecond
	data := blobOf(2000, 'w')
	digest := f.addBlob(data)
	m, srv := newTestMirror(t, f, nil)
	url := srv.URL + "/v2/library/alpine/blobs/" + digest

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if resp, err := testClient.Do(req); err == nil {
		resp.Body.Close()
		t.Fatal("the client was expected to give up before the first byte")
	}

	deadline := time.Now().Add(3 * time.Second)
	for m.Stats().BytesStored != int64(len(data)) {
		if time.Now().After(deadline) {
			t.Fatalf("%d bytes kept after the client left, want %d", m.Stats().BytesStored, len(data))
		}
		time.Sleep(10 * time.Millisecond)
	}
	do(t, http.MethodGet, url, nil)
	if n := f.count("cdn"); n != 1 {
		t.Errorf("the content network was asked %d times, want 1", n)
	}
}

// Nobody is owed a blob that is not going to be kept, so a download of one stops
// when the client does. Otherwise a job could start a download of every large
// layer there is and walk away from each.
func TestAClientThatGoesAwayStopsTheDownloadOfABlobTooLargeToKeep(t *testing.T) {
	f := newFakeRegistry(t)
	f.setStall(true)
	digest := f.addBlob(blobOf(3000, 'l'))
	_, srv := newTestMirror(t, f, func(c *Config) { c.MaxBytes = 4000 })
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/v2/library/alpine/blobs/"+digest, nil)
	resp, err := testClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 100)
	if _, err := resp.Body.Read(buf); err != nil {
		t.Fatalf("no first bytes: %v", err)
	}

	cancel()
	resp.Body.Close()

	deadline := time.Now().Add(3 * time.Second)
	for f.cdnEnded.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the download from upstream was still going 3 seconds after the client left")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestARangeRequestIsServedFromDiskOrPassedThroughButNeverKept(t *testing.T) {
	f := newFakeRegistry(t)
	data := make([]byte, 3000)
	for i := range data {
		data[i] = byte(i % 251)
	}
	digest := f.addBlob(data)
	m, srv := newTestMirror(t, f, nil)
	url := srv.URL + "/v2/library/alpine/blobs/" + digest
	rangeOf := func(spec string) http.Header { return http.Header{"Range": {spec}} }

	body, status, err := fetch(url, rangeOf("bytes=0-9"))
	if err != nil || status != http.StatusPartialContent || !bytes.Equal(body, data[:10]) {
		t.Fatalf("a range of a blob not held: status %d, error %v, %d bytes, want 206 and the first 10", status, err, len(body))
	}
	if got := m.Stats().BytesStored; got != 0 {
		t.Errorf("a range request kept %d bytes: a partial blob is not the blob", got)
	}

	do(t, http.MethodGet, url, nil)
	before := f.count("cdn")
	body, status, err = fetch(url, rangeOf("bytes=5-14"))

	if err != nil || status != http.StatusPartialContent || !bytes.Equal(body, data[5:15]) {
		t.Fatalf("a range of a blob held: status %d, error %v, %d bytes, want 206 and bytes 5 to 14", status, err, len(body))
	}
	if f.count("cdn") != before {
		t.Error("a range of a blob the mirror holds went upstream")
	}
}

// A manifest is read whole before it is sent, so a registry that stalls in the
// middle of one would hold the request open for as long as the client let it.
func TestAnUpstreamThatStallsMidManifestIsGivenUpOn(t *testing.T) {
	f := newFakeRegistry(t)
	digest := f.addManifest("library/alpine", "", manifestBody("slow"), dockerV2)
	f.setManifestStall(true)
	_, srv := newTestMirror(t, f, func(c *Config) { c.IdleTimeout = 150 * time.Millisecond })

	start := time.Now()
	_, status, err := fetch(srv.URL+"/v2/library/alpine/manifests/"+digest, nil)

	if err != nil || status != http.StatusBadGateway {
		t.Errorf("status %d, error %v, want 502", status, err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("giving up took %v, want about the 150ms idle timeout", d)
	}
}

// The idle timer is armed when the transfer starts, not when its first byte
// arrives. A registry that sends its headers and then nothing is the case where
// the difference shows.
func TestAnUpstreamThatSendsItsHeadersAndNothingElseIsGivenUpOn(t *testing.T) {
	f := newFakeRegistry(t)
	digest := f.addBlob(blobOf(4000, 'n'))
	f.setStall(true)
	f.cdnStallEmpty = true
	m, srv := newTestMirror(t, f, func(c *Config) { c.IdleTimeout = 150 * time.Millisecond })

	start := time.Now()
	_, _, err := fetch(srv.URL+"/v2/library/alpine/blobs/"+digest, nil)

	if err == nil {
		t.Error("the client was told a blob arrived whole")
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("giving up took %v, want about the 150ms idle timeout", d)
	}
	if got := m.Stats().UpstreamErrors; got == 0 {
		t.Error("no upstream error was counted")
	}
}

// The token is a small JSON answer, and a service that sends its headers and
// then stalls would hold the request open for good: the transport bounds the
// wait for headers and not for a body. It matters more than for a blob, because
// the fetch of a blob is not stopped by the client leaving, so a hung token
// request would hold every request for that blob.
func TestATokenServiceThatStallsMidAnswerIsGivenUpOn(t *testing.T) {
	f := newFakeRegistry(t)
	digest := f.addBlob(blobOf(500, 't'))
	f.setTokenStall(true)
	_, srv := newTestMirror(t, f, func(c *Config) { c.IdleTimeout = 150 * time.Millisecond })

	start := time.Now()
	body, status, err := fetch(srv.URL+"/v2/library/alpine/blobs/"+digest, nil)

	if err != nil || status != http.StatusBadGateway {
		t.Errorf("status %d, error %v, %d bytes, want 502", status, err, len(body))
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("giving up took %v, want about the 150ms timeout", d)
	}
}
