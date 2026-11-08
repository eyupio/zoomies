package cacheservice

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/xml"
	"io"
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
	if r.Method == http.MethodGet && r.Header.Get("Range") == "" {
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
	limit := s.opts.MaxBytes
	if limit <= 0 || limit > maxEntryBytes {
		limit = maxEntryBytes
	}
	body := http.MaxBytesReader(w, r.Body, limit)
	defer body.Close()

	q := r.URL.Query()
	switch q.Get("comp") {
	case "":
		// A small archive: one PUT is the whole thing.
		if err := writeFileAtomic(s.pendingData(id), body); err != nil {
			s.failUpload(w, p.ID, err)
			return
		}
		s.setSize(id)
	case "block":
		blockID := q.Get("blockid")
		if blockID == "" {
			http.Error(w, "blockid is required", http.StatusBadRequest)
			return
		}
		if err := os.MkdirAll(s.pendingBlocks(id), 0o750); err != nil {
			s.failUpload(w, p.ID, err)
			return
		}
		// The id is the SDK's base64 text; hex keeps it a safe file name.
		if err := writeFileAtomic(filepath.Join(s.pendingBlocks(id), hex.EncodeToString([]byte(blockID))), body); err != nil {
			s.failUpload(w, p.ID, err)
			return
		}
	case "blocklist":
		var list struct {
			IDs []string `xml:",any"`
		}
		raw, err := io.ReadAll(io.LimitReader(body, maxJSONBody))
		if err != nil || xml.Unmarshal(raw, &list) != nil {
			http.Error(w, "the block list could not be read", http.StatusBadRequest)
			return
		}
		if err := s.assemble(id, list.IDs); err != nil {
			s.failUpload(w, p.ID, err)
			return
		}
		s.setSize(id)
	default:
		http.Error(w, "unsupported operation", http.StatusNotImplemented)
		return
	}
	h := w.Header()
	h.Set("ETag", `"0x`+id+`"`)
	h.Set("x-ms-request-server-encrypted", "true")
	w.WriteHeader(http.StatusCreated)
}

// assemble joins the staged blocks, in the order the commit lists them, into
// the archive. A block the list names that was never staged fails the upload.
func (s *Service) assemble(id string, blocks []string) error {
	out, err := os.CreateTemp(s.pendingPath(id), "assemble-*")
	if err != nil {
		return err
	}
	defer os.Remove(out.Name())
	for _, b := range blocks {
		in, err := os.Open(filepath.Join(s.pendingBlocks(id), hex.EncodeToString([]byte(strings.TrimSpace(b)))))
		if err != nil {
			_ = out.Close()
			return err
		}
		_, err = io.Copy(out, in)
		_ = in.Close()
		if err != nil {
			_ = out.Close()
			return err
		}
	}
	if err := out.Close(); err != nil {
		return err
	}
	if err := os.Rename(out.Name(), s.pendingData(id)); err != nil {
		return err
	}
	return os.RemoveAll(s.pendingBlocks(id))
}

// setSize records how long the whole archive is, once it is whole, so the
// finalise call can be checked against the bytes actually received.
func (s *Service) setSize(id string) {
	fi, err := os.Stat(s.pendingData(id))
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.pending[id]; ok && err == nil {
		p.Size = fi.Size()
	}
}

// failUpload gives the reservation up. The job sees a failed save, which the
// toolkit reports as a warning; nothing half-written stays behind.
func (s *Service) failUpload(w http.ResponseWriter, id string, err error) {
	s.log.Warn("a cache upload failed and was dropped", "error", err)
	s.mu.Lock()
	s.dropPendingLocked(id)
	s.mu.Unlock()
	http.Error(w, "the upload could not be stored", http.StatusInternalServerError)
}

// writeFileAtomic writes to a temporary name beside the target and renames it,
// so a reader of the target sees all of a file or none of it.
func writeFileAtomic(path string, src io.Reader) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".upload-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, src); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
