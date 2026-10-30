package backup

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/cryptox"
)

// A backup that leaves the machine.
//
// A copy beside the database is a backup against a mistake: a bad migration, a
// deletion somebody regrets, an upgrade that went sideways. It is not a backup
// against the disk, the machine or the datacentre, and a directory of tidy
// copies on the volume that just died is the worst kind of backup because
// everyone believed in it.
//
// So a remote is one S3-compatible bucket. Every successful copy is exactly one
// object: a plaintext manifest header and then the encrypted archive. Old
// copies written as a single `.tar.gz[.enc]` object, or as a `.sealed` and
// `.json` pair, are still listed, fetched and deleted; new copies use the
// header form and age the old ones out through retention.

// ErrNoRemote is a remote named by something that does not have one.
var ErrNoRemote = errors.New("backup: no such backup remote")

// Remote is one configured destination, ready to be used.
type Remote struct {
	cfg config.BackupRemote
	s3  *s3Client
	// innerPassphrase seals the payload before a destination passphrase, if
	// any, seals it again. It is the instance key when the caller has one,
	// which is what makes the payload readable only on a host holding the key
	// the backup's manifest names.
	innerPassphrase  string
	innerFingerprint string
}

// NewRemote builds a client for one configured remote. It has no instance key,
// so it can only seal a new payload with the remote's own passphrase; uploading
// to a remote with neither is refused rather than writing a readable database.
func NewRemote(cfg config.BackupRemote, httpClient *http.Client) (*Remote, error) {
	return NewRemoteWithKey(cfg, httpClient, nil)
}

// NewRemoteWithKey builds a client whose payloads are sealed with key as well
// as any destination passphrase.
func NewRemoteWithKey(cfg config.BackupRemote, httpClient *http.Client, key *cryptox.Key) (*Remote, error) {
	client, err := newS3Client(cfg.Endpoint, cfg.Region, cfg.Bucket, cfg.AccessKeyID, cfg.SecretAccessKey, cfg.PathStyle, httpClient)
	if err != nil {
		return nil, fmt.Errorf("the backup remote %s: %w", cfg.Name, err)
	}
	r := &Remote{cfg: cfg, s3: client}
	if key != nil {
		r.innerPassphrase = key.Encode()
		r.innerFingerprint = key.Fingerprint()
	}
	return r, nil
}

// Name is what this destination is called.
func (r *Remote) Name() string { return r.cfg.Name }

// Where is the bucket and prefix, for a log line or a row on the page.
func (r *Remote) Where() string { return r.cfg.Where() }

// Endpoint is the service it talks to.
func (r *Remote) Endpoint() string { return r.cfg.Endpoint }

// Encrypted says the archive is sealed before it leaves this host. New copies
// always are; the method remains for the page and for old copies.
func (r *Remote) Encrypted() bool { return true }

// Keep is how many copies this remote holds; 0 keeps every one.
func (r *Remote) Keep() int { return r.cfg.Keep }

// Compressed says whether a new payload is gzipped before it is encrypted.
func (r *Remote) Compressed() bool { return r.cfg.Compressed() }

// Key is where a new backup with this id lands in the bucket.
func (r *Remote) Key(id string) string {
	name := id + ".gz.enc"
	if !r.Compressed() {
		name = id + ".enc"
	}
	return r.join(name)
}

func (r *Remote) join(name string) string {
	if r.cfg.Prefix == "" {
		return name
	}
	return strings.Trim(r.cfg.Prefix, "/") + "/" + name
}

// Copy is one backup as a bucket holds it: an archive, not a directory.
type Copy struct {
	// Remote is the destination's name, so a list gathered from several says
	// which came from where.
	Remote string `json:"remote"`
	// ID is the backup's own id, read back out of the key, so a copy in a
	// bucket and the copy on disk it came from have one name.
	ID string `json:"id"`
	// Key is the object's full key, which is what an operator needs to find
	// it with any other tool.
	Key string `json:"key"`
	// Bytes is the archive's size, which is the compressed database rather
	// than the database.
	Bytes int64 `json:"bytes"`
	// StoredAt is when the object was written, from the service. It is not
	// when the backup was taken -- an old backup uploaded today is stored
	// today -- and the id carries the other.
	StoredAt time.Time `json:"stored_at"`
	// TakenAt is read from the id, which is the only thing about the backup a
	// listing can know without downloading it.
	TakenAt time.Time `json:"taken_at"`
	// Encrypted says the archive is sealed and needs the remote's passphrase
	// or the instance key.
	Encrypted bool `json:"encrypted"`
	// Compression is "gzip" for a compressed copy and empty otherwise.
	Compression string `json:"compression,omitempty"`

	// objects is every key this copy occupies. A new copy is one; an old
	// `.sealed` plus `.json` pair is two.
	objects []string
}

// Check asks the bucket one cheap question -- list one key under the prefix --
// which is the whole of what has to work for a backup to reach it: the
// endpoint resolves, TLS agrees, the signature is accepted, the bucket exists,
// and the policy allows reading it.
func (r *Remote) Check(ctx context.Context) error {
	_, err := r.s3.list(ctx, r.cfg.Prefix, 1)
	return err
}

// List is every backup this remote holds, newest first.
//
// It works from the key listing alone: an object's key is enough to name the
// copy and its age, so a bucket with a thousand copies costs one listing
// request, not a thousand downloads.
func (r *Remote) List(ctx context.Context) ([]Copy, error) {
	dir := r.dir()
	listPrefix := r.cfg.Prefix
	if dir != "." {
		listPrefix = dir + "/"
	}
	objects, err := r.s3.list(ctx, listPrefix, 0)
	if err != nil {
		return nil, err
	}

	held := map[string]*foundCopy{}
	sealed := map[string]s3Object{}
	manifests := map[string]s3Object{}
	for _, o := range objects {
		if path.Dir(o.Key) != dir {
			continue
		}
		pk, ok := parseRemoteKey(o.Key)
		if !ok {
			continue
		}
		switch pk.kind {
		case remoteNew:
			r.consider(held, pk.id, o, Copy{
				Remote: r.cfg.Name, ID: pk.id, Key: o.Key, Bytes: o.Size,
				StoredAt: o.LastModified, TakenAt: takenFromRemoteName(pk.id),
				Encrypted: true, Compression: pk.compression,
				objects: []string{o.Key},
			}, 3)
		case remoteArchive:
			compression := ""
			if pk.compressed {
				compression = "gzip"
			}
			r.consider(held, pk.id, o, Copy{
				Remote: r.cfg.Name, ID: pk.id, Key: o.Key, Bytes: o.Size,
				StoredAt: o.LastModified, TakenAt: takenFromRemoteName(pk.id),
				Encrypted: pk.encrypted, Compression: compression,
				objects: []string{o.Key},
			}, 2)
		case remoteSealed:
			sealed[pk.id] = o
		case remoteManifest:
			manifests[pk.id] = o
		}
	}
	for id, s := range sealed {
		m, ok := manifests[id]
		if !ok {
			continue
		}
		r.consider(held, id, s, Copy{
			Remote: r.cfg.Name, ID: id, Key: s.Key, Bytes: s.Size,
			StoredAt: s.LastModified, TakenAt: takenFromRemoteName(id),
			Encrypted: true, objects: []string{s.Key, m.Key},
		}, 1)
	}
	out := make([]Copy, 0, len(held))
	for _, h := range held {
		out = append(out, h.copy)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].TakenAt.Equal(out[j].TakenAt) {
			return out[i].TakenAt.After(out[j].TakenAt)
		}
		return out[i].ID > out[j].ID
	})
	return out, nil
}

type foundCopy struct {
	copy     Copy
	priority int
}

func (r *Remote) consider(held map[string]*foundCopy, id string, o s3Object, c Copy, priority int) {
	if existing, ok := held[id]; ok {
		if existing.priority >= priority {
			return
		}
	}
	if c.StoredAt.IsZero() {
		c.StoredAt = o.LastModified
	}
	held[id] = &foundCopy{copy: c, priority: priority}
}

// dir is the directory Key puts this remote's archives in, in the form
// path.Dir reports for an object key: "." for the bucket's root.
func (r *Remote) dir() string {
	if dir := strings.Trim(r.cfg.Prefix, "/"); dir != "" {
		return dir
	}
	return "."
}

type remoteKeyKind int

const (
	remoteNew remoteKeyKind = iota
	remoteArchive
	remoteSealed
	remoteManifest
)

type parsedRemoteKey struct {
	id          string
	kind        remoteKeyKind
	compressed  bool
	encrypted   bool
	compression string
}

// parseRemoteKey reads a copy's name and layout from its key. The suffix
// order matters: ".tar.gz.enc" also ends in ".gz.enc", and is an old copy.
func parseRemoteKey(key string) (parsedRemoteKey, bool) {
	name := path.Base(key)
	switch {
	case strings.HasSuffix(name, ".tar.gz.enc"):
		id := strings.TrimSuffix(name, ".tar.gz.enc")
		if !validRemoteName(id) {
			return parsedRemoteKey{}, false
		}
		return parsedRemoteKey{id: id, kind: remoteArchive, compressed: true, encrypted: true, compression: "gzip"}, true
	case strings.HasSuffix(name, ".tar.gz"):
		id := strings.TrimSuffix(name, ".tar.gz")
		if !validRemoteName(id) {
			return parsedRemoteKey{}, false
		}
		return parsedRemoteKey{id: id, kind: remoteArchive, compressed: true, compression: "gzip"}, true
	case strings.HasSuffix(name, ".gz.enc"):
		id := strings.TrimSuffix(name, ".gz.enc")
		if !validRemoteName(id) {
			return parsedRemoteKey{}, false
		}
		return parsedRemoteKey{id: id, kind: remoteNew, compressed: true, encrypted: true, compression: "gzip"}, true
	case strings.HasSuffix(name, ".enc"):
		id := strings.TrimSuffix(name, ".enc")
		if !validRemoteName(id) {
			return parsedRemoteKey{}, false
		}
		return parsedRemoteKey{id: id, kind: remoteNew, encrypted: true}, true
	case strings.HasSuffix(name, ".sealed"):
		id := strings.TrimSuffix(name, ".sealed")
		if !validRemoteName(id) {
			return parsedRemoteKey{}, false
		}
		return parsedRemoteKey{id: id, kind: remoteSealed, encrypted: true}, true
	case strings.HasSuffix(name, ".json"):
		id := strings.TrimSuffix(name, ".json")
		if !validRemoteName(id) {
			return parsedRemoteKey{}, false
		}
		return parsedRemoteKey{id: id, kind: remoteManifest}, true
	}
	return parsedRemoteKey{}, false
}

var remoteStamp = regexp.MustCompile(`(\d{8}-\d{6})(?:-\d+)?$`)

// validRemoteName is the name grammar a copy may have. It is deliberately
// broader than ValidID, which names local directories: an older copy may carry
// any target prefix before its timestamp, but the timestamp at the end is what
// makes retention safe.
func validRemoteName(id string) bool {
	if id == "" || strings.ContainsAny(id, `/\`) || strings.HasPrefix(id, ".") {
		return false
	}
	m := remoteStamp.FindStringSubmatch(id)
	if m == nil {
		return false
	}
	_, err := time.Parse("20060102-150405", m[1])
	return err == nil
}

// takenFromRemoteName reads the instant out of a remote copy's name.
func takenFromRemoteName(id string) time.Time {
	m := remoteStamp.FindStringSubmatch(id)
	if m == nil {
		return time.Time{}
	}
	t, err := time.Parse("20060102-150405", m[1])
	if err != nil {
		return time.Time{}
	}
	return t
}

// Upload copies one backup into the bucket as one object and returns what
// landed there.
//
// The payload is written to a file first: its manifest must name the payload's
// size and digest, and neither is knowable until the last chunk has been
// produced. The object is then hash-verified whole before a single PUT, or
// sent in parts when it is larger than the store's single-PUT limit. Nothing
// is ever held in memory except metadata.
func (r *Remote) Upload(ctx context.Context, entry *Entry) (*Copy, error) {
	inner := strings.TrimSpace(r.innerPassphrase)
	if inner == "" {
		inner = strings.TrimSpace(r.cfg.Passphrase)
	}
	if inner == "" {
		inner = keyPassphraseFromEntry(entry)
	}
	if inner == "" {
		return nil, fmt.Errorf("backup: %s must encrypt every copy before it leaves this host; set a passphrase on the remote or run where the instance encryption key is available", r.cfg.Name)
	}
	outer := strings.TrimSpace(r.cfg.Passphrase)
	compressed := r.Compressed()

	payload, err := os.CreateTemp(filepath.Dir(entry.Dir), ".offsite-payload-*")
	if err != nil {
		return nil, fmt.Errorf("backup: spooling %s for %s: %w", entry.ID, r.cfg.Name, err)
	}
	defer func() {
		payload.Close()
		_ = os.Remove(payload.Name())
	}()

	var (
		outerDigest *digestWriter
		outerEnc    io.WriteCloser
	)
	innerDest := io.Writer(payload)
	if outer != "" {
		outerDigest = newDigestWriter(payload)
		outerEnc, err = NewEncryptor(outerDigest, outer)
		if err != nil {
			return nil, err
		}
		innerDest = outerEnc
	}
	innerDigest := newDigestWriter(innerDest)
	innerEnc, err := NewEncryptor(innerDigest, inner)
	if err != nil {
		return nil, err
	}
	plainSize, plainSHA, err := writeArchive(innerEnc, entry, compressed)
	if err != nil {
		return nil, err
	}
	if err := innerEnc.Close(); err != nil {
		return nil, err
	}
	if outerEnc != nil {
		if err := outerEnc.Close(); err != nil {
			return nil, err
		}
	}

	om := objectManifestFor(entry, compressed, r.innerFingerprint)
	om.PlaintextSize, om.PlaintextSHA256 = plainSize, plainSHA
	om.PayloadSize, om.PayloadSHA256 = innerDigest.n, innerDigest.hex()
	if outerDigest != nil {
		om.PassphraseSealed = true
		om.OuterSize, om.OuterSHA256 = outerDigest.n, outerDigest.hex()
	}
	payloadSize, err := payload.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, err
	}
	if _, err := payload.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	object, err := os.CreateTemp(filepath.Dir(entry.Dir), ".offsite-object-*")
	if err != nil {
		return nil, fmt.Errorf("backup: spooling the object for %s: %w", r.cfg.Name, err)
	}
	defer func() {
		object.Close()
		_ = os.Remove(object.Name())
	}()
	whole := newDigestWriter(object)
	if err := WriteObjectHeader(whole, om); err != nil {
		return nil, err
	}
	if _, err := io.Copy(whole, io.LimitReader(payload, payloadSize)); err != nil {
		return nil, err
	}
	size := whole.n
	if _, err := object.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	key := r.Key(entry.ID)
	if err := r.s3.putObject(ctx, key, object, size, whole.hex(), r.s3.multipartThreshold); err != nil {
		return nil, err
	}
	return &Copy{
		Remote: r.cfg.Name, ID: entry.ID, Key: key, Bytes: size,
		StoredAt: time.Now().UTC(), TakenAt: entry.TakenAt, Encrypted: true,
		Compression: om.Compression, objects: []string{key},
	}, nil
}

// keyPassphraseFromEntry is the last resort for a caller that built a Remote
// without the instance key: the backup's own manifest says where its key came
// from, and the copy may carry it. Reading it here is the same act as passing
// the key to NewRemoteWithKey, just discovered from the copy being uploaded.
func keyPassphraseFromEntry(entry *Entry) string {
	if entry == nil {
		return ""
	}
	if entry.Manifest != nil {
		if key := keyPassphraseFromPath(entry.Manifest.Key.SourcePath); key != "" {
			return key
		}
	}
	return keyPassphraseFromPath(filepath.Join(entry.Dir, KeyName))
}

func keyPassphraseFromPath(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	key, err := cryptox.LoadKeyFile(path)
	if err != nil {
		return ""
	}
	return key.Encode()
}

// FetchOptions says who is pulling a copy back and how the archive opens.
type FetchOptions struct {
	// Passphrase overrides the remote's own, for an archive sealed with a
	// passphrase this configuration no longer carries -- which is what a
	// rotation leaves behind in a bucket.
	Passphrase string
	// TakenBy is recorded in the manifest of the copy that lands on disk.
	TakenBy string
	// MaxBytes bounds the unpacked database. Zero is unbounded.
	MaxBytes int64
	// Now is the clock; nil uses the wall clock.
	Now func() time.Time
}

// Fetch pulls one copy out of the bucket and unpacks it into root, where it is
// an ordinary backup: listed, verifiable, and restorable by the same staging
// the Backups tab does everything else through.
func (r *Remote) Fetch(ctx context.Context, root, id string, opts FetchOptions) (*Entry, error) {
	if !ValidID(id) {
		return nil, fmt.Errorf("%w: %q", ErrInvalidID, id)
	}
	copied, err := r.find(ctx, id)
	if err != nil {
		return nil, err
	}
	body, _, err := r.s3.get(ctx, copied.Key)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	return r.fetchFrom(ctx, root, copied.ID, copied, body, opts)
}

func (r *Remote) fetchFrom(ctx context.Context, root, id string, copied Copy, body io.Reader, opts FetchOptions) (*Entry, error) {
	bundled, src := IsBundledObject(body)
	// An old .sealed object may itself be a bundled object if a name was reused
	// by a newer controller; the bytes, not the key suffix, decide.
	if bundled {
		return r.fetchObject(ctx, root, src, opts)
	}
	if copied.KeyKind() == remoteSealed {
		// The manifest beside the old sealed object is not needed to unpack it,
		// but reading it first keeps a corrupt pair from being half-accepted.
		if _, err := r.readOldManifest(ctx, copied); err != nil {
			return nil, err
		}
	}

	passphrase := r.passphraseFor(opts)
	encrypted, peek := IsEncrypted(src)
	stream := peek
	if encrypted {
		key := passphrase
		if strings.TrimSpace(key) == "" {
			key = strings.TrimSpace(r.innerPassphrase)
		}
		if strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("backup: %s holds an encrypted copy and no passphrase or key was given", r.cfg.Name)
		}
		opened, err := NewDecryptor(peek, key)
		if err != nil {
			return nil, err
		}
		stream = opened
	}
	entry, err := Unpack(ctx, root, stream, UnpackOptions{
		Source: SourceFetched, TakenBy: opts.TakenBy, MaxBytes: opts.MaxBytes, Now: opts.Now,
	})
	if err != nil {
		return nil, r.explain(err, id)
	}
	if _, err := io.Copy(io.Discard, stream); err != nil {
		return nil, r.explain(err, id)
	}
	return entry, nil
}

// fetchObject reconstructs a copy from the one-object layout.
func (r *Remote) fetchObject(ctx context.Context, root string, body io.Reader, opts FetchOptions) (*Entry, error) {
	om, err := ReadObjectHeader(body)
	if err != nil {
		return nil, err
	}
	raw := newObservedReader(body)
	var innerStream io.Reader = raw
	if om.PassphraseSealed {
		passphrase := r.passphraseFor(opts)
		if strings.TrimSpace(passphrase) == "" {
			return nil, fmt.Errorf("backup: the copy in %s was sealed with its destination passphrase; give it with --passphrase-file or configure it on the remote", r.cfg.Name)
		}
		innerStream, err = NewDecryptor(raw, passphrase)
		if err != nil {
			return nil, r.explain(err, om.Target)
		}
	}
	inner := newObservedReader(innerStream)
	key := strings.TrimSpace(r.innerPassphrase)
	if key == "" {
		key = r.passphraseFor(opts)
	}
	if key == "" {
		key = keyPassphraseFromPath(om.Key.SourcePath)
	}
	if key == "" {
		return nil, fmt.Errorf("backup: this copy was sealed with the instance key %s; this host holds no key to open it", om.KeyFingerprint)
	}
	plain, err := NewDecryptor(inner, key)
	if err != nil {
		if om.PassphraseSealed && errors.Is(err, ErrNotEncrypted) {
			return nil, fmt.Errorf("%w: the passphrase for %s does not open the outer seal of %s", ErrWrongPassphrase, r.cfg.Name, om.Target)
		}
		return nil, r.explain(err, om.Target)
	}
	entry, err := Unpack(ctx, root, plain, UnpackOptions{
		Source: SourceFetched, TakenBy: opts.TakenBy, MaxBytes: opts.MaxBytes,
		Uncompressed: om.Compression == "", Now: opts.Now,
	})
	if err != nil {
		return nil, r.explain(err, om.Target)
	}
	// The final encrypted chunk is authentication, not data: the tar reader
	// stops before it, so it is drained here and its absence would otherwise
	// turn a truncated copy into a shorter accepted one.
	if _, err := io.Copy(io.Discard, plain); err != nil {
		_ = os.RemoveAll(entry.Dir)
		return nil, r.explain(err, om.Target)
	}
	fail := func(err error) (*Entry, error) {
		_ = os.RemoveAll(entry.Dir)
		return nil, err
	}
	if err := inner.matches(om.PayloadSize, om.PayloadSHA256); err != nil {
		return fail(err)
	}
	if om.PassphraseSealed {
		if err := raw.matches(om.OuterSize, om.OuterSHA256); err != nil {
			return fail(err)
		}
	} else if err := raw.matches(om.PayloadSize, om.PayloadSHA256); err != nil {
		return fail(err)
	}
	return entry, nil
}

func (r *Remote) passphraseFor(opts FetchOptions) string {
	if strings.TrimSpace(opts.Passphrase) != "" {
		return opts.Passphrase
	}
	return strings.TrimSpace(r.cfg.Passphrase)
}

func (r *Remote) explain(err error, id string) error {
	if errors.Is(err, ErrWrongPassphrase) {
		return fmt.Errorf("%w: the passphrase or key for %s does not open the copy %s", ErrWrongPassphrase, r.cfg.Name, id)
	}
	return err
}

// find locates one copy from the key listing. It never downloads a copy to
// list it, and it prefers the one-object layout when both exist.
func (r *Remote) find(ctx context.Context, id string) (Copy, error) {
	copies, err := r.List(ctx)
	if err != nil {
		return Copy{}, err
	}
	for _, c := range copies {
		if c.ID == id {
			return c, nil
		}
	}
	return Copy{}, fmt.Errorf("backup: %s holds no copy called %s", r.cfg.Name, id)
}

// KeyKind reports which layout a copy was found in.
func (c Copy) KeyKind() remoteKeyKind {
	for _, key := range c.objects {
		pk, ok := parseRemoteKey(key)
		if ok {
			return pk.kind
		}
	}
	pk, _ := parseRemoteKey(c.Key)
	return pk.kind
}

// ReadHeader reads only the plaintext manifest at the front of a copy. It is
// what a page needs to label a remote copy without downloading a database.
func (r *Remote) ReadHeader(ctx context.Context, id string) (*ObjectManifest, error) {
	if !ValidID(id) {
		return nil, fmt.Errorf("%w: %q", ErrInvalidID, id)
	}
	copied, err := r.find(ctx, id)
	if err != nil {
		return nil, err
	}
	switch copied.KeyKind() {
	case remoteNew:
		return r.readObjectHeader(ctx, copied.Key)
	case remoteSealed:
		return r.readOldManifest(ctx, copied)
	default:
		return nil, fmt.Errorf("backup: the copy %s in %s predates the manifest header; fetch it to read its manifest", id, r.cfg.Name)
	}
}

// ReadManifest is ReadHeader under the name the rest of the package uses.
func (r *Remote) ReadManifest(ctx context.Context, id string) (*ObjectManifest, error) {
	return r.ReadHeader(ctx, id)
}

func (r *Remote) readObjectHeader(ctx context.Context, key string) (*ObjectManifest, error) {
	fixed, _, err := r.s3.getRange(ctx, key, 0, ObjectHeaderLen()-1)
	if err != nil {
		return nil, err
	}
	head := make([]byte, ObjectHeaderLen())
	_, err = io.ReadFull(fixed, head)
	fixed.Close()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadObjectHeader, err)
	}
	if string(head[:len(ObjectMagic)]) != ObjectMagic {
		return nil, ErrBadObjectHeader
	}
	n := int64(binaryBigEndianUint32(head[len(ObjectMagic) : len(ObjectMagic)+4]))
	if n == 0 || n > MaxObjectManifest {
		return nil, fmt.Errorf("%w: the manifest length is %d bytes", ErrBadObjectHeader, n)
	}
	full, _, err := r.s3.getRange(ctx, key, 0, ObjectHeaderLen()+n-1)
	if err != nil {
		return nil, err
	}
	defer full.Close()
	return ReadObjectHeader(full)
}

func (r *Remote) readOldManifest(ctx context.Context, copied Copy) (*ObjectManifest, error) {
	for _, key := range copied.objects {
		pk, ok := parseRemoteKey(key)
		if !ok || pk.kind != remoteManifest {
			continue
		}
		body, _, err := r.s3.get(ctx, key)
		if err != nil {
			return nil, err
		}
		raw, err := io.ReadAll(io.LimitReader(body, MaxObjectManifest+1))
		body.Close()
		if err != nil {
			return nil, err
		}
		if len(raw) > MaxObjectManifest {
			return nil, ErrManifestTooLarge
		}
		var m Manifest
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, fmt.Errorf("backup: %s is not a manifest: %w", key, err)
		}
		om := objectManifestFor(&Entry{ID: copied.ID, TakenAt: m.TakenAt, Manifest: &m}, false, "")
		om.TakenAt = m.TakenAt
		return om, nil
	}
	return nil, fmt.Errorf("backup: %s holds no manifest beside the sealed copy %s", r.cfg.Name, copied.ID)
}

// Delete removes one copy from the bucket. An old copy written as two objects
// has both removed; the rest of the bucket and prefix are untouched.
func (r *Remote) Delete(ctx context.Context, id string) error {
	if !ValidID(id) {
		return fmt.Errorf("%w: %q", ErrInvalidID, id)
	}
	return r.deleteCopy(ctx, Copy{ID: id, Key: r.Key(id), objects: []string{r.Key(id)}})
}

// deleteCopy removes every layout one backup name can occupy: its one-object
// key, the older single archive, and the even older sealed/manifest pair. A
// delete is a statement about the name, not about which layout a listing
// happened to prefer.
func (r *Remote) deleteCopy(ctx context.Context, copied Copy) error {
	keys := append([]string{}, copied.objects...)
	keys = append(keys,
		r.Key(copied.ID),
		r.join(copied.ID+".gz.enc"),
		r.join(copied.ID+".enc"),
		r.join(copied.ID+".tar.gz.enc"),
		r.join(copied.ID+".tar.gz"),
		r.join(copied.ID+".sealed"),
		r.join(copied.ID+".json"),
	)
	seen := map[string]bool{}
	for _, key := range keys {
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		if err := r.s3.remove(ctx, key); err != nil {
			return err
		}
	}
	return nil
}

// Prune deletes all but the newest keep copies and returns the ids it removed.
// Zero keeps every one.
func (r *Remote) Prune(ctx context.Context, keep int) ([]string, error) {
	if keep <= 0 {
		return nil, nil
	}
	copies, err := r.List(ctx)
	if err != nil {
		return nil, err
	}
	if len(copies) <= keep {
		return nil, nil
	}
	var removed []string
	for _, c := range copies[keep:] {
		if err := r.deleteCopy(ctx, c); err != nil {
			return removed, err
		}
		removed = append(removed, c.ID)
	}
	return removed, nil
}

// Missing is the backups in entries that this remote does not already hold,
// newest first and no more than the remote keeps.
func Missing(entries []Entry, held []Copy, keep int) []Entry {
	have := map[string]bool{}
	for _, c := range held {
		have[c.ID] = true
	}
	var ours []Entry
	for _, e := range entries {
		// A backup with no database, or one this fleet did not take, is not
		// something to put in somebody's bucket by itself.
		if e.Problem != "" || e.Source == SourcePreMigration {
			continue
		}
		ours = append(ours, e)
	}
	if keep > 0 && len(ours) > keep {
		ours = ours[:keep]
	}
	out := []Entry{}
	for _, e := range ours {
		if !have[e.ID] {
			out = append(out, e)
		}
	}
	// Oldest first, so an interrupted catch-up leaves the bucket holding a
	// contiguous run rather than a gap.
	slices.Reverse(out)
	return out
}

// putObject writes one object with a single PUT when it fits, and as a
// multipart upload when it does not. The whole-object digest is computed by
// the caller before this is called.
func (c *s3Client) putObject(ctx context.Context, key string, body io.ReaderAt, size int64, digest string, threshold int64) error {
	if threshold <= 0 {
		threshold = s3MultipartThreshold
	}
	if size <= threshold {
		return c.put(ctx, key, io.NewSectionReader(body, 0, size), size, digest)
	}
	uploadID, err := c.createMultipart(ctx, key)
	if err != nil {
		return err
	}
	var parts []completedPart
	for number, offset := 1, int64(0); offset < size; number, offset = number+1, offset+threshold {
		n := min(threshold, size-offset)
		part := io.NewSectionReader(body, offset, n)
		h := sha256.New()
		if _, err := io.Copy(h, part); err != nil {
			_ = c.abortMultipart(context.WithoutCancel(ctx), key, uploadID)
			return err
		}
		partDigest := hex.EncodeToString(h.Sum(nil))
		etag, err := c.uploadPart(ctx, key, uploadID, number, io.NewSectionReader(body, offset, n), n, partDigest)
		if err != nil {
			_ = c.abortMultipart(context.WithoutCancel(ctx), key, uploadID)
			return err
		}
		parts = append(parts, completedPart{Number: number, Size: n, ETag: etag})
	}
	if err := c.completeMultipart(ctx, key, uploadID, parts); err != nil {
		// The service may have completed the upload and lost the answer. Ask
		// for the object before deciding: an upload left open is charged for,
		// and a copy advertised but absent is worse.
		if got, herr := c.head(context.WithoutCancel(ctx), key); herr == nil && got == size {
			return nil
		}
		_ = c.abortMultipart(context.WithoutCancel(ctx), key, uploadID)
		return err
	}
	return nil
}

// observedReader hashes and counts everything that passes through it, so a
// payload and its manifest can be compared after unpacking without buffering
// the payload.
type observedReader struct {
	r io.Reader
	h hash.Hash
	n int64
}

func newObservedReader(r io.Reader) *observedReader {
	return &observedReader{r: r, h: sha256.New()}
}

func (o *observedReader) Read(p []byte) (int, error) {
	n, err := o.r.Read(p)
	if n > 0 {
		o.h.Write(p[:n])
		o.n += int64(n)
	}
	return n, err
}

func (o *observedReader) matches(size int64, digest string) error {
	if size >= 0 && o.n != size {
		return fmt.Errorf("backup: the copy is %d bytes but its manifest says %d", o.n, size)
	}
	if digest != "" && !strings.EqualFold(fmt.Sprintf("%x", o.h.Sum(nil)), digest) {
		return errors.New("backup: the copy is not the bytes its manifest recorded: its digest has changed")
	}
	return nil
}

func binaryBigEndianUint32(b []byte) uint32 { return binary.BigEndian.Uint32(b) }
