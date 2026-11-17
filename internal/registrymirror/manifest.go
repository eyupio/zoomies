package registrymirror

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// maxManifestBytes is the most a manifest may be: the registry specification's
// own limit. A manifest is held in memory while it is checked, so it is bounded
// here rather than by the folder.
const maxManifestBytes = 4 << 20

// defaultMaxTags bounds what is remembered about tags, which is held in memory
// and chosen by whoever sends the requests.
const defaultMaxTags = 10_000

// tagKey is what an answer about a tag is for. What a registry answers can
// depend on what the client says it accepts, so that is part of it.
type tagKey struct{ name, tag, accept string }

type tagEntry struct {
	digest      string
	contentType string
	size        int64
	checked     time.Time
}

func (m *Mirror) tagCount() int {
	m.tagMu.Lock()
	defer m.tagMu.Unlock()
	return len(m.tags)
}

// serveManifest answers a request for a manifest by digest or by tag.
//
// A digest names exact bytes, so what is held under it never goes stale. A tag
// is a pointer that moves, and is the only mutable thing the mirror knows: it
// is remembered for a short time and then asked about again with a HEAD, which
// is cheap, and the manifest is fetched only when the digest it now points at is
// one the mirror does not hold.
func (m *Mirror) serveManifest(w http.ResponseWriter, r *http.Request, rt route) {
	accept := strings.Join(r.Header.Values("Accept"), ", ")
	digest := rt.ref
	if !isDigest(rt.ref) {
		te, apiErr := m.resolveTag(r, rt, accept)
		if apiErr != nil {
			m.fail(w, apiErr)
			return
		}
		digest = te.digest
		if r.Method == http.MethodHead {
			h := w.Header()
			h.Set("Content-Type", te.contentType)
			h.Set("Docker-Content-Digest", te.digest)
			if te.size >= 0 {
				h.Set("Content-Length", strconv.FormatInt(te.size, 10))
			}
			w.WriteHeader(http.StatusOK)
			return
		}
	}
	if obj, ok := m.store.open(digest); ok {
		defer obj.release()
		m.hits.Add(1)
		serveStored(w, r, obj, digest)
		return
	}
	m.misses.Add(1)
	body, contentType, apiErr := m.fetchManifest(r, rt.name, digest, accept)
	if apiErr != nil {
		m.fail(w, apiErr)
		return
	}
	h := w.Header()
	h.Set("Content-Type", contentType)
	h.Set("Docker-Content-Digest", digest)
	h.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

func acceptHeader(accept string) http.Header {
	if accept == "" {
		return nil
	}
	return http.Header{"Accept": {accept}}
}

// resolveTag says what digest a tag points at: from what is remembered while it
// is fresh, and otherwise by asking the registry with a HEAD.
func (m *Mirror) resolveTag(r *http.Request, rt route, accept string) (tagEntry, *apiError) {
	key := tagKey{rt.name, rt.ref, accept}
	now := m.cfg.Now()
	m.tagMu.Lock()
	if e, ok := m.tags[key]; ok && now.Sub(e.checked) < m.cfg.TagTTL {
		m.tagMu.Unlock()
		return e, nil
	}
	m.tagMu.Unlock()

	resp, err := m.registryDo(r.Context(), http.MethodHead, "/v2/"+rt.name+"/manifests/"+rt.ref, rt.name, acceptHeader(accept))
	if err != nil {
		return tagEntry{}, unavailable(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return tagEntry{}, upstreamStatusError(resp, "MANIFEST_UNKNOWN")
	}
	digest := resp.Header.Get("Docker-Content-Digest")
	if !validDigest(digest) {
		return tagEntry{}, unavailable(errors.New("the registry gave no digest for the tag"))
	}
	e := tagEntry{digest: digest, contentType: resp.Header.Get("Content-Type"), size: resp.ContentLength, checked: now}
	m.remember(key, e, now)
	return e, nil
}

// remember keeps an answer about a tag, and makes room first if the bound has
// been reached: expired answers go, and then the oldest.
func (m *Mirror) remember(key tagKey, e tagEntry, now time.Time) {
	m.tagMu.Lock()
	defer m.tagMu.Unlock()
	if _, ok := m.tags[key]; !ok {
		for len(m.tags) >= m.maxTags {
			var oldest tagKey
			var oldestAt time.Time
			first := true
			for k, v := range m.tags {
				if now.Sub(v.checked) >= m.cfg.TagTTL {
					oldest, first = k, false
					break
				}
				if first || v.checked.Before(oldestAt) {
					oldest, oldestAt, first = k, v.checked, false
				}
			}
			delete(m.tags, oldest)
		}
	}
	m.tags[key] = e
}

// fetchManifest gets a manifest by digest from the registry, checks it hashes to
// that digest, keeps it and returns it. The bytes are in memory while they are
// checked, so nothing is sent to the client that has not been.
func (m *Mirror) fetchManifest(r *http.Request, name, digest, accept string) ([]byte, string, *apiError) {
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	resp, err := m.registryDo(ctx, http.MethodGet, "/v2/"+name+"/manifests/"+digest, name, acceptHeader(accept))
	if err != nil {
		return nil, "", unavailable(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", upstreamStatusError(resp, "MANIFEST_UNKNOWN")
	}
	body, err := io.ReadAll(io.LimitReader(m.idle(resp.Body, cancel), maxManifestBytes+1))
	if err != nil {
		return nil, "", unavailable(err)
	}
	m.bytesFetched.Add(int64(len(body)))
	if len(body) > maxManifestBytes {
		return nil, "", unavailable(fmt.Errorf("the manifest is over the %d byte limit", maxManifestBytes))
	}
	sum := sha256.Sum256(body)
	if got := "sha256:" + hex.EncodeToString(sum[:]); got != digest {
		m.cfg.Log.Warn("registry mirror refused a manifest that did not match its digest", "digest", digest, "got", got)
		return nil, "", unavailable(errors.New("the manifest does not hash to its digest"))
	}
	contentType := resp.Header.Get("Content-Type")
	if !m.store.cacheable(int64(len(body))) {
		return body, contentType, nil // served, and not kept: it is too large to
	}
	if keep, err := m.store.begin(); err != nil {
		m.cfg.Log.Warn("registry mirror cannot keep a manifest", "error", err)
	} else if _, err := keep.Write(body); err != nil {
		keep.abort()
		m.cfg.Log.Warn("registry mirror cannot keep a manifest", "error", err)
	} else if err := keep.commit(digest, contentType); err != nil {
		m.cfg.Log.Warn("registry mirror cannot keep a manifest", "error", err)
	}
	return body, contentType, nil
}
