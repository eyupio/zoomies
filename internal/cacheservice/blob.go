package cacheservice

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// maxEntryBytes is the toolkit's own ceiling on one archive, used when the
// service has no ceiling of its own, so a request can never write without bound.
const maxEntryBytes = 10 << 30

// blob serves the one URL every entry has. The toolkit saves with Azure's
// Blob Storage SDK, which makes one PUT for a small archive and, for a large
// one, stages blocks and then commits their list; it restores with plain GETs,
// ranged for a large archive. Only those few shapes are answered.
func (s *Service) blob(w http.ResponseWriter, r *http.Request, rest string) {
	// Azure puts these on every reply, and its Go SDK reads them without a
	// nil check: BuildKit's cache client dereferences the request id after an
	// upload, so a reply without one panics the builder instead of failing
	// the save.
	var rid [16]byte
	_, _ = rand.Read(rid[:])
	h := w.Header()
	h.Set("x-ms-request-id", hex.EncodeToString(rid[:]))
	h.Set("x-ms-version", "2021-08-06")
	id, _, _ := strings.Cut(rest, "/")
	if id == "" || strings.ContainsAny(id, `./\`) {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodPut:
		s.putBlob(w, r, id)
	case http.MethodGet, http.MethodHead:
		s.getBlob(w, r, id)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Service) getBlob(w http.ResponseWriter, r *http.Request, id string) {
	s.mu.Lock()
	e, ok := s.entries[id]
	s.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(s.blobPath(e.ID))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	// Azure's clients name the byte range x-ms-range, not Range, and BuildKit's
	// asks for the middle of a layer that way. http.ServeContent only knows Range,
	// so without this a read from the middle of a blob is answered with the
	// whole of it, and the layer comes out wrong.
	if ms := r.Header.Get("x-ms-range"); ms != "" && r.Header.Get("Range") == "" {
		r.Header.Set("Range", ms)
	}
	// A ranged read is a use too: BuildKit and the toolkit's large downloads are
	// all ranged, so counting only whole reads left the busiest entries looking
	// idle to the eviction order.
	if r.Method == http.MethodGet {
		s.touch(e.ID)
	}
	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	h.Set("x-ms-blob-type", "BlockBlob")
	h.Set("ETag", `"0x`+e.ID+`"`)
	http.ServeContent(w, r, "", fi.ModTime(), f)
}

func (s *Service) putBlob(w http.ResponseWriter, r *http.Request, id string) {
	s.mu.Lock()
	p, ok := s.pending[id]
	s.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}

	q := r.URL.Query()
	switch q.Get("comp") {
	case "":
		// A small archive: one PUT is the whole thing.
		n, err := s.stage(w, r, p, s.pendingData(id), nil)
		if err != nil {
			s.failUpload(w, p.ID, err)
			return
		}
		if !s.record(p, func() { p.data, p.Size = n, n }) {
			http.NotFound(w, r)
			return
		}
	case "block":
		blockID := q.Get("blockid")
		if blockID == "" {
			http.Error(w, "blockid is required", http.StatusBadRequest)
			return
		}
		// The id is the SDK's base64 text; hex keeps it a safe file name.
		name := hex.EncodeToString([]byte(blockID))
		s.mu.Lock()
		over := len(p.blocks) >= s.maxBlocks()
		_, replacing := p.blocks[name]
		s.mu.Unlock()
		if over && !replacing {
			s.failUpload(w, p.ID, errors.New("an upload staged more blocks than the service allows"))
			return
		}
		// Mkdir, not MkdirAll: if the reservation expired a moment ago its folder
		// is gone, and recreating it would leave bytes nothing will ever reclaim.
		if err := os.Mkdir(s.pendingBlocks(id), 0o750); err != nil && !errors.Is(err, fs.ErrExist) {
			if errors.Is(err, fs.ErrNotExist) {
				http.NotFound(w, r)
				return
			}
			s.failUpload(w, p.ID, err)
			return
		}
		n, err := s.stage(w, r, p, filepath.Join(s.pendingBlocks(id), name), &name)
		if err != nil {
			s.failUpload(w, p.ID, err)
			return
		}
		if !s.record(p, func() {
			if p.blocks == nil {
				p.blocks = map[string]int64{}
			}
			p.blocks[name] = n
		}) {
			http.NotFound(w, r)
			return
		}
	case "blocklist":
		var list struct {
			IDs []string `xml:",any"`
		}
		raw, err := io.ReadAll(io.LimitReader(http.MaxBytesReader(w, r.Body, maxJSONBody), maxJSONBody))
		if err != nil || xml.Unmarshal(raw, &list) != nil {
			http.Error(w, "the block list could not be read", http.StatusBadRequest)
			return
		}
		n, err := s.assemble(p, list.IDs)
		if err != nil {
			s.failUpload(w, p.ID, err)
			return
		}
		if !s.record(p, func() { p.data, p.Size, p.blocks = n, n, nil }) {
			http.NotFound(w, r)
			return
		}
	default:
		http.Error(w, "unsupported operation", http.StatusNotImplemented)
		return
	}
	h := w.Header()
	h.Set("ETag", `"0x`+id+`"`)
	h.Set("x-ms-request-server-encrypted", "true")
	w.WriteHeader(http.StatusCreated)
}

// stage writes the request body to path, refusing more than one archive may be
// and more than the pool's open uploads have room for. replaces names the block
// this write overwrites (nil for the whole archive), whose bytes are about to be
// freed and so do not count against the room.
func (s *Service) stage(w http.ResponseWriter, r *http.Request, p *pending, path string, replaces *string) (int64, error) {
	s.mu.Lock()
	var freed int64
	if replaces != nil {
		freed = p.blocks[*replaces]
	} else {
		freed = p.data
	}
	_, staged := s.pendingTotalsLocked()
	room := s.maxPendingBytes() - (staged - freed)
	s.mu.Unlock()
	limit := min(s.entryLimit(), room)
	if limit <= 0 {
		return 0, errors.New("the service's open uploads have no room left")
	}
	body := http.MaxBytesReader(w, r.Body, limit)
	defer body.Close()
	return writeFileAtomic(path, body)
}

// record applies a finished write to the reservation, but only if the
// reservation is still open. One that expired, or was abandoned, while its
// bytes were arriving has had its folder removed; what was just written is
// removed again and the caller answers as for an unknown upload.
func (s *Service) record(p *pending, apply func()) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cur, ok := s.pending[p.ID]; !ok || cur != p {
		_ = os.RemoveAll(s.pendingPath(p.ID))
		return false
	}
	apply()
	return true
}

// assemble joins the staged blocks, in the order the commit lists them, into
// the archive, and returns its length. A block the list names that was never
// staged fails the upload, and so does a list whose blocks add up to more than
// an archive may be: the sum is taken from the sizes recorded as each block
// arrived, before a byte is copied, because a list may name one block again and
// again and each mention is another copy.
func (s *Service) assemble(p *pending, blocks []string) (int64, error) {
	if len(blocks) > s.maxBlocks() {
		return 0, errors.New("a block list names more blocks than the service allows")
	}
	names := make([]string, len(blocks))
	var total, staged int64
	s.mu.Lock()
	for i, b := range blocks {
		names[i] = hex.EncodeToString([]byte(strings.TrimSpace(b)))
		n, ok := p.blocks[names[i]]
		if !ok {
			s.mu.Unlock()
			return 0, fmt.Errorf("block %q was never staged", strings.TrimSpace(b))
		}
		total += n
		if total > s.entryLimit() {
			s.mu.Unlock()
			return 0, errors.New("a block list adds up to more than an archive may be")
		}
	}
	_, all := s.pendingTotalsLocked()
	staged = all - p.stagedBytes() + total
	s.mu.Unlock()
	// The blocks are removed once joined, so the result is what stays; the join
	// itself briefly holds both, which the open-upload bound leaves headroom for.
	if staged > s.maxPendingBytes() {
		return 0, errors.New("the service's open uploads have no room for this archive")
	}

	out, err := os.CreateTemp(s.pendingPath(p.ID), "assemble-*")
	if err != nil {
		return 0, err
	}
	defer os.Remove(out.Name())
	for _, name := range names {
		in, err := os.Open(filepath.Join(s.pendingBlocks(p.ID), name))
		if err != nil {
			_ = out.Close()
			return 0, err
		}
		_, err = io.Copy(out, in)
		_ = in.Close()
		if err != nil {
			_ = out.Close()
			return 0, err
		}
	}
	if err := out.Close(); err != nil {
		return 0, err
	}
	if err := os.Rename(out.Name(), s.pendingData(p.ID)); err != nil {
		return 0, err
	}
	return total, os.RemoveAll(s.pendingBlocks(p.ID))
}

// failUpload gives the reservation up. The job sees a failed save, which the
// toolkit reports as a warning; nothing half-written stays behind.
func (s *Service) failUpload(w http.ResponseWriter, id string, err error) {
	s.log.Warn("a cache upload failed and was dropped", "error", err)
	s.mu.Lock()
	s.abandonPendingLocked(id)
	s.mu.Unlock()
	http.Error(w, "the upload could not be stored", http.StatusInternalServerError)
}

// writeFileAtomic writes to a temporary name beside the target and renames it,
// so a reader of the target sees all of a file or none of it.
func writeFileAtomic(path string, src io.Reader) (int64, error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".upload-*")
	if err != nil {
		return 0, err
	}
	defer os.Remove(tmp.Name())
	n, err := io.Copy(tmp, src)
	if err != nil {
		_ = tmp.Close()
		return 0, err
	}
	if err := tmp.Close(); err != nil {
		return 0, err
	}
	return n, os.Rename(tmp.Name(), path)
}
