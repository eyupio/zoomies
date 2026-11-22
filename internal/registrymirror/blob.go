package registrymirror

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// flight is a blob being fetched. A second request for it waits for the first
// rather than starting another: eight jobs starting at once on a host pull the
// same base image once.
type flight struct {
	done chan struct{}
	once sync.Once
}

func (m *Mirror) joinFlight(digest string) (*flight, bool) {
	m.flightMu.Lock()
	defer m.flightMu.Unlock()
	if fl, ok := m.flights[digest]; ok {
		return fl, false
	}
	fl := &flight{done: make(chan struct{})}
	m.flights[digest] = fl
	return fl, true
}

func (m *Mirror) endFlight(digest string, fl *flight) {
	fl.once.Do(func() {
		m.flightMu.Lock()
		if m.flights[digest] == fl {
			delete(m.flights, digest)
		}
		m.flightMu.Unlock()
		close(fl.done)
	})
}

// serveBlob answers a request for a blob: from disk if the mirror holds it,
// and otherwise by fetching it once, keeping it and serving it in the same
// pass.
func (m *Mirror) serveBlob(w http.ResponseWriter, r *http.Request, rt route) {
	for {
		if obj, ok := m.store.open(rt.ref); ok {
			defer obj.release()
			m.hits.Add(1)
			serveStored(w, r, obj, rt.ref)
			return
		}
		switch {
		case r.Method == http.MethodHead:
			m.headBlob(w, r, rt)
			return
		case r.Header.Get("Range") != "":
			m.passRange(w, r, rt)
			return
		}
		fl, leader := m.joinFlight(rt.ref)
		if leader {
			m.misses.Add(1)
			m.fetchAndServe(w, r, rt, fl)
			return
		}
		select {
		case <-fl.done:
		case <-r.Context().Done():
			return
		}
	}
}

// serveStored serves an object from its file. ServeContent does the ranges and
// the HEAD, and is given no modification time because there is none worth
// telling a client about.
func serveStored(w http.ResponseWriter, r *http.Request, obj held, digest string) {
	ct := obj.contentType
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Docker-Content-Digest", digest)
	http.ServeContent(w, r, "", time.Time{}, obj.file)
}

// headBlob answers a HEAD for a blob the mirror does not hold, by asking
// upstream the same and passing the answer on, without fetching any bytes.
func (m *Mirror) headBlob(w http.ResponseWriter, r *http.Request, rt route) {
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	resp, apiErr := m.openBlob(ctx, cancel, http.MethodHead, rt, nil)
	if apiErr != nil {
		m.fail(w, apiErr)
		return
	}
	defer resp.Body.Close()
	m.upstreamAnswered() // a HEAD has no body to wait for
	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	h.Set("Docker-Content-Digest", rt.ref)
	if resp.ContentLength >= 0 {
		h.Set("Content-Length", strconv.FormatInt(resp.ContentLength, 10))
	}
	w.WriteHeader(http.StatusOK)
}

// openBlob asks the registry for a blob and follows the redirect it answers
// with to wherever the bytes are, sending that second request no credential.
// The body it returns gives up if it makes no progress for the idle timeout.
func (m *Mirror) openBlob(ctx context.Context, cancel context.CancelFunc, method string, rt route, header http.Header) (*http.Response, *apiError) {
	ranged := header.Get("Range") != ""
	resp, err := m.registryDo(ctx, method, "/v2/"+rt.name+"/blobs/"+rt.ref, rt.name, header)
	if err != nil {
		return nil, unavailable(err)
	}
	if blobOK(resp.StatusCode, ranged) {
		resp.Body = m.idle(resp.Body, cancel)
		return resp, nil
	}
	defer resp.Body.Close()
	if !isRedirect(resp.StatusCode) {
		return nil, upstreamStatusError(resp, "BLOB_UNKNOWN")
	}
	loc, err := resp.Location()
	if err != nil {
		return nil, unavailable(err)
	}
	req, err := http.NewRequestWithContext(ctx, method, loc.String(), nil)
	if err != nil {
		return nil, unavailable(err)
	}
	if ranged {
		req.Header.Set("Range", header.Get("Range"))
	}
	second, err := m.redirect.Do(req)
	if err != nil {
		return nil, unavailable(err)
	}
	if !blobOK(second.StatusCode, ranged) {
		defer second.Body.Close()
		return nil, upstreamStatusError(second, "BLOB_UNKNOWN")
	}
	second.Body = m.idle(second.Body, cancel)
	return second, nil
}

// clientStall is how long one write to a client may take before the client is
// given up on. The idle timeout cancels a download that has stopped arriving, but
// cancelling does not unblock a write to a client that has stopped reading, so
// without this a download stopped where it was and the flight for its blob stayed
// open for as long as the connection did. It is half the idle timeout, so that the
// client is given up on while the download is still moving and not after it has
// been cancelled for standing still, and it applies to one write of a chunk, so a
// slow client that is reading is not touched.
func (m *Mirror) clientStall() time.Duration { return m.cfg.IdleTimeout / 2 }

func blobOK(status int, ranged bool) bool {
	return status == http.StatusOK || ranged && status == http.StatusPartialContent
}

// passRange answers a request for part of a blob the mirror does not hold, by
// passing it on. A part of a blob is not the blob, so nothing is kept.
func (m *Mirror) passRange(w http.ResponseWriter, r *http.Request, rt route) {
	m.misses.Add(1)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	resp, apiErr := m.openBlob(ctx, cancel, http.MethodGet, rt, http.Header{"Range": {r.Header.Get("Range")}})
	if apiErr != nil {
		m.fail(w, apiErr)
		return
	}
	defer resp.Body.Close()
	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	h.Set("Docker-Content-Digest", rt.ref)
	for _, k := range []string{"Content-Range", "Content-Length"} {
		if v := resp.Header.Get(k); v != "" {
			h.Set(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	rc := http.NewResponseController(w)
	buf := make([]byte, 64<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			m.bytesFetched.Add(int64(n))
			_ = rc.SetWriteDeadline(time.Now().Add(m.clientStall()))
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			_ = rc.Flush()
		}
		if rerr == io.EOF {
			// A part of a blob has no digest to be checked against, so its
			// arriving whole is the most the mirror can say.
			m.upstreamAnswered()
			return
		}
		if rerr != nil {
			m.upstreamError()
			panic(http.ErrAbortHandler)
		}
	}
}

// fetchAndServe is the first request for a blob: it fetches it, sends it to its
// client as it arrives, and keeps it if it hashes to the digest asked for.
//
// Whoever asked first may go away. If the blob is going to be kept the
// download is finished anyway, because the next job is owed it; if it is not
// going to be kept nobody is, and the download stops with the client.
func (m *Mirror) fetchAndServe(w http.ResponseWriter, r *http.Request, rt route, fl *flight) {
	defer m.endFlight(rt.ref, fl)
	ctx, cancel := context.WithCancel(context.WithoutCancel(r.Context()))
	defer cancel()
	var keeping atomic.Bool
	keeping.Store(true) // until it is known that the blob is too large to keep
	stop := context.AfterFunc(r.Context(), func() {
		if !keeping.Load() {
			cancel()
		}
	})
	defer stop()
	giveUp := func() {
		keeping.Store(false)
		m.endFlight(rt.ref, fl) // nobody need wait for a blob that will not be kept
		if r.Context().Err() != nil {
			cancel()
		}
	}

	resp, apiErr := m.openBlob(ctx, cancel, http.MethodGet, rt, nil)
	if apiErr != nil {
		m.fail(w, apiErr)
		return
	}
	defer resp.Body.Close()

	// A blob too large to keep is sent on and not kept. When the registry gives
	// no length the same rule is applied as the bytes arrive.
	var keep *pending
	if resp.ContentLength < 0 || m.store.cacheable(resp.ContentLength) {
		var err error
		if keep, err = m.store.begin(); err != nil {
			m.cfg.Log.Warn("registry mirror cannot keep a blob", "error", err)
			keep = nil
		}
	}
	if keep == nil {
		giveUp()
	}

	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	h.Set("Docker-Content-Digest", rt.ref)
	if resp.ContentLength >= 0 {
		h.Set("Content-Length", strconv.FormatInt(resp.ContentLength, 10))
	}
	w.WriteHeader(http.StatusOK)

	rc := http.NewResponseController(w)
	sum := sha256.New()
	buf := make([]byte, 64<<10)
	var read int64
	clientGone := false
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			read += int64(n)
			m.bytesFetched.Add(int64(n))
			sum.Write(chunk)
			if keep != nil {
				if _, werr := keep.Write(chunk); werr != nil {
					m.cfg.Log.Warn("registry mirror stopped keeping a blob", "error", werr)
					keep.abort()
					keep = nil
					giveUp()
				} else if !m.store.cacheable(keep.size) {
					keep.abort()
					keep = nil
					giveUp()
				}
			}
			// When the length is known this is the last chunk once it has all
			// arrived. It is checked, and the blob kept, before the client is
			// sent it: a blob that does not match its digest is then never
			// delivered whole, and what is kept is on disk by the time the
			// client has everything.
			if resp.ContentLength >= 0 && read == resp.ContentLength {
				if got := "sha256:" + hex.EncodeToString(sum.Sum(nil)); got != rt.ref {
					m.cfg.Log.Warn("registry mirror dropped a blob that did not match its digest", "digest", rt.ref, "got", got)
					if keep != nil {
						keep.abort()
					}
					m.upstreamError()
					panic(http.ErrAbortHandler)
				}
				m.upstreamAnswered()
				if keep != nil {
					if err := keep.commit(rt.ref, ""); err != nil {
						m.cfg.Log.Warn("registry mirror could not keep a blob", "digest", rt.ref, "error", err)
					}
					keep = nil
				}
			}
			if !clientGone {
				_ = rc.SetWriteDeadline(time.Now().Add(m.clientStall()))
				if _, werr := w.Write(chunk); werr != nil {
					// Gone, or not reading for longer than a write may take. Either
					// way it is given up on and the download goes on for whoever
					// keeps the blob and waits for it; one that nobody keeps has no
					// reason to go on.
					clientGone = true
					if !keeping.Load() {
						cancel()
					}
				} else {
					_ = rc.Flush()
				}
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			if keep != nil {
				keep.abort()
			}
			m.upstreamError()
			// The client has been told the blob is coming. Ending the response
			// cleanly would tell it the blob is whole, so cut the connection.
			panic(http.ErrAbortHandler)
		}
	}
	if keep == nil {
		if resp.ContentLength < 0 {
			// Not kept, because it was too large or could not be written, but every
			// chunk was hashed as it arrived, so the digest can still be checked.
			// The client has the blob whole already and checks it for itself.
			if got := "sha256:" + hex.EncodeToString(sum.Sum(nil)); got != rt.ref {
				m.cfg.Log.Warn("registry mirror was sent a blob that did not match its digest", "digest", rt.ref, "got", got)
				m.upstreamError()
			} else {
				m.upstreamAnswered()
			}
		}
		return
	}
	// Reached for a blob of unknown length, or an empty one, where there was no
	// last chunk to hold back.
	complete := resp.ContentLength < 0 || read == resp.ContentLength
	if got := "sha256:" + hex.EncodeToString(sum.Sum(nil)); !complete || got != rt.ref {
		m.cfg.Log.Warn("registry mirror dropped a blob that did not match its digest", "digest", rt.ref, "got", got)
		keep.abort()
		m.upstreamError()
		return
	}
	m.upstreamAnswered()
	if err := keep.commit(rt.ref, ""); err != nil {
		m.cfg.Log.Warn("registry mirror could not keep a blob", "digest", rt.ref, "error", err)
	}
}
