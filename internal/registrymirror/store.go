package registrymirror

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// maxKeptFraction is the share of the bound one object may take: a blob larger
// than max/maxKeptFraction is sent to the client and not kept, so one enormous
// image cannot push everything else out.
const maxKeptFraction = 4

// store keeps blobs and manifests under their digests, in files, and knows how
// much it holds. Nothing becomes visible in it until it has been written to a
// temporary name and its hash has matched the digest it was asked for.
type store struct {
	dir string
	max int64
	now func() time.Time

	mu        sync.Mutex
	entries   map[string]*entry
	used      int64
	evictions int64
}

type entry struct {
	size        int64
	lastUsed    time.Time
	contentType string
	// readers is how many clients are being served from the object. An object
	// being read is never evicted.
	readers int
}

func newStore(dir string, max int64, now func() time.Time) (*store, error) {
	for _, d := range []string{filepath.Join(dir, "objects"), filepath.Join(dir, "tmp")} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			return nil, err
		}
	}
	s := &store{dir: dir, max: max, now: now, entries: map[string]*entry{}}
	if err := s.scan(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.evictLocked("")
	s.mu.Unlock()
	return s, nil
}

// scan reads what a previous run left. Whatever was being written when it
// stopped is not an object and is removed, as is a content type whose object is
// not there; what is complete is counted, so the bound holds across a restart.
// Objects are only ever renamed into place whole, so they are trusted by name.
func (s *store) scan() error {
	tmp, err := os.ReadDir(filepath.Join(s.dir, "tmp"))
	if err != nil {
		return err
	}
	for _, e := range tmp {
		_ = os.Remove(filepath.Join(s.dir, "tmp", e.Name()))
	}
	shards, err := os.ReadDir(filepath.Join(s.dir, "objects"))
	if err != nil {
		return err
	}
	for _, shard := range shards {
		if !shard.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(s.dir, "objects", shard.Name()))
		if err != nil {
			return err
		}
		for _, file := range files {
			name := file.Name()
			full := filepath.Join(s.dir, "objects", shard.Name(), name)
			if strings.HasSuffix(name, typeSuffix) {
				if _, err := os.Stat(strings.TrimSuffix(full, typeSuffix)); err != nil {
					_ = os.Remove(full)
				}
				continue
			}
			info, err := file.Info()
			if err != nil {
				continue
			}
			ct, _ := os.ReadFile(full + typeSuffix)
			s.entries["sha256:"+name] = &entry{size: info.Size(), lastUsed: info.ModTime(), contentType: string(ct)}
			s.used += info.Size()
		}
	}
	return nil
}

// cacheable says whether an object of this size may be kept at all.
func (s *store) cacheable(size int64) bool { return size <= s.max/maxKeptFraction }

// evictLocked removes the least recently used objects that nobody is reading
// until the store is within its bound. The object just kept is not a candidate:
// it is what the room was made for.
func (s *store) evictLocked(except string) {
	for s.used > s.max {
		victim := ""
		var oldest time.Time
		for digest, e := range s.entries {
			if e.readers > 0 || digest == except {
				continue
			}
			if victim == "" || e.lastUsed.Before(oldest) {
				victim, oldest = digest, e.lastUsed
			}
		}
		if victim == "" {
			return
		}
		path := s.path(victim)
		_ = os.Remove(path + typeSuffix)
		_ = os.Remove(path)
		s.used -= s.entries[victim].size
		delete(s.entries, victim)
		s.evictions++
	}
}

func (s *store) evicted() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.evictions
}

// typeSuffix names the file beside an object that holds its content type.
const typeSuffix = ".type"

// path is where an object lives: sharded by the first two hex digits so no
// directory holds every object.
func (s *store) path(digest string) string {
	hexPart := strings.TrimPrefix(digest, "sha256:")
	return filepath.Join(s.dir, "objects", hexPart[:2], hexPart)
}

// held is an object opened for serving. Its release must be called when it has
// been served.
type held struct {
	file        *os.File
	size        int64
	contentType string
	release     func()
}

// open returns a held object.
func (s *store) open(digest string) (held, bool) {
	s.mu.Lock()
	e, ok := s.entries[digest]
	if !ok {
		s.mu.Unlock()
		return held{}, false
	}
	e.lastUsed = s.now()
	e.readers++
	size, ct := e.size, e.contentType
	s.mu.Unlock()
	release := func() {
		s.mu.Lock()
		e.readers--
		s.mu.Unlock()
	}
	f, err := os.Open(s.path(digest))
	if err != nil {
		release()
		return held{}, false
	}
	return held{file: f, size: size, contentType: ct, release: func() { _ = f.Close(); release() }}, true
}

func (s *store) stored() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.used
}

// pending is an object being written.
type pending struct {
	s    *store
	file *os.File
	sum  hash.Hash
	size int64
}

func (s *store) begin() (*pending, error) {
	f, err := os.CreateTemp(filepath.Join(s.dir, "tmp"), "obj-*.tmp")
	if err != nil {
		return nil, err
	}
	return &pending{s: s, file: f, sum: sha256.New()}, nil
}

func (p *pending) Write(b []byte) (int, error) {
	n, err := p.file.Write(b)
	p.sum.Write(b[:n])
	p.size += int64(n)
	return n, err
}

func (p *pending) abort() {
	name := p.file.Name()
	_ = p.file.Close()
	_ = os.Remove(name)
}

// commit makes the object visible under digest, if and only if what was
// written hashes to it. The content type is kept beside it, because a manifest
// is served with the type the registry gave it.
func (p *pending) commit(digest, contentType string) error {
	if got := "sha256:" + hex.EncodeToString(p.sum.Sum(nil)); got != digest {
		p.abort()
		return fmt.Errorf("content hashes to %s, not %s", got, digest)
	}
	name := p.file.Name()
	if err := p.file.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	final := p.s.path(digest)
	if err := os.MkdirAll(filepath.Dir(final), 0o750); err != nil {
		_ = os.Remove(name)
		return err
	}
	if contentType != "" {
		if err := os.WriteFile(final+typeSuffix, []byte(contentType), 0o640); err != nil {
			_ = os.Remove(name)
			return err
		}
	}
	if err := os.Rename(name, final); err != nil {
		_ = os.Remove(name)
		return err
	}
	p.s.mu.Lock()
	defer p.s.mu.Unlock()
	if old, ok := p.s.entries[digest]; ok {
		p.s.used -= old.size
	}
	p.s.entries[digest] = &entry{size: p.size, lastUsed: p.s.now(), contentType: contentType}
	p.s.used += p.size
	p.s.evictLocked(digest)
	return nil
}
