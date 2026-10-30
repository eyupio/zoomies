package backup

import (
	"bufio"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"strings"
	"time"
)

// A backup that leaves this host is one object: a small plaintext manifest,
// then the encrypted archive it describes. Keeping both inside the object is
// what makes the format portable across every S3-compatible store: user
// metadata and tags are extensions an implementation may ignore or rewrite,
// and a sidecar object is a second thing that can be missing.
const (
	// ObjectMagic begins every object this package writes.
	ObjectMagic = "ZPBKUP01"
	// MaxObjectManifest bounds the plaintext header. The manifest names what
	// the payload is; it is not where the database lives.
	MaxObjectManifest = 1 << 20
	// objectHeaderFixed is the magic, the length and the digest.
	objectHeaderFixed = len(ObjectMagic) + 4 + sha256.Size
)

// ErrBadObjectHeader is an object whose header is missing, truncated, or not
// the manifest its digest says it is.
var ErrBadObjectHeader = errors.New("backup: this object does not have a usable backup header")

// ErrManifestTooLarge is a header that would need more than MaxObjectManifest
// bytes, which this package never writes.
var ErrManifestTooLarge = errors.New("backup: the backup manifest is larger than 1 MiB")

// ObjectManifest is the plaintext description at the front of a stored copy.
//
// It says what the payload is and how to verify it, and nothing that opens it:
// no passphrase, credential, token or private key belongs here. The database
// file's own manifest travels inside the encrypted payload, where it belongs.
type ObjectManifest struct {
	ManifestVersion int       `json:"manifest_version"`
	Target          string    `json:"target"`
	TakenAt         time.Time `json:"taken_at"`
	Source          string    `json:"source,omitempty"`
	TakenBy         string    `json:"taken_by,omitempty"`

	// PlaintextSize and PlaintextSHA256 describe the uncompressed archive the
	// payload was made from, before compression and encryption.
	PlaintextSize   int64  `json:"plaintext_size"`
	PlaintextSHA256 string `json:"plaintext_sha256"`

	// PayloadSize and PayloadSHA256 describe the encrypted stream that follows
	// the header: compression has already happened, encryption has not.
	PayloadSize   int64  `json:"payload_size"`
	PayloadSHA256 string `json:"payload_sha256"`

	// Compression is "gzip" when the plaintext was compressed before it was
	// encrypted, and empty otherwise.
	Compression string `json:"compression,omitempty"`

	// KeyFingerprint is the key that sealed the database's secrets, when the
	// backup's own manifest names one. It is an identifier, never key material.
	KeyFingerprint string `json:"key_fingerprint,omitempty"`

	// PassphraseSealed says a destination passphrase was applied over the
	// already-encrypted payload. OuterSize and OuterSHA256 describe that outer
	// layer, which is the bytes after the header in that case.
	PassphraseSealed bool   `json:"passphrase_sealed,omitempty"`
	OuterSize        int64  `json:"outer_size,omitempty"`
	OuterSHA256      string `json:"outer_sha256,omitempty"`

	// Version, Commit and Instance identify what wrote the copy without
	// requiring the payload to be opened.
	Version  string `json:"version,omitempty"`
	Commit   string `json:"commit,omitempty"`
	Instance string `json:"instance,omitempty"`

	// Database and Secrets are copied from the backup's own manifest, so a
	// listing can say what the copy contains and which key it needs. They hold
	// no secret value.
	Database Database `json:"database"`
	Key      KeyInfo  `json:"key"`
	Secrets  []Secret `json:"secrets,omitempty"`
}

// objectManifestFor builds the plaintext description of one archive.
func objectManifestFor(entry *Entry, compressed bool, innerFingerprint string) *ObjectManifest {
	m := &ObjectManifest{
		ManifestVersion: ManifestVersion,
		Target:          strings.TrimSuffix(DirPrefix, "-"),
		TakenAt:         entry.TakenAt,
		Compression:     "",
	}
	if compressed {
		m.Compression = "gzip"
	}
	if !m.TakenAt.IsZero() {
		m.TakenAt = m.TakenAt.UTC()
	}
	if h, err := os.Hostname(); err == nil {
		m.Instance = h
	}
	if entry.Manifest != nil {
		bm := entry.Manifest
		m.ManifestVersion = bm.ManifestVersion
		if !bm.TakenAt.IsZero() {
			m.TakenAt = bm.TakenAt.UTC()
		}
		m.Source = bm.Source
		m.TakenBy = bm.TakenBy
		m.Version = bm.Zoomies.Version
		m.Commit = bm.Zoomies.Commit
		m.Database = bm.Database
		m.Key = bm.Key
		m.Secrets = bm.Secrets
		m.KeyFingerprint = bm.Key.Fingerprint
	}
	if innerFingerprint != "" {
		m.KeyFingerprint = innerFingerprint
	}
	return m
}

// WriteObjectHeader writes the fixed header and the manifest JSON. The caller
// writes the payload immediately after it.
func WriteObjectHeader(w io.Writer, m *ObjectManifest) error {
	raw, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if len(raw) > MaxObjectManifest {
		return ErrManifestTooLarge
	}
	sum := sha256.Sum256(raw)
	if _, err := io.WriteString(w, ObjectMagic); err != nil {
		return err
	}
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(raw)))
	if _, err := w.Write(length[:]); err != nil {
		return err
	}
	if _, err := w.Write(sum[:]); err != nil {
		return err
	}
	_, err = w.Write(raw)
	return err
}

// ReadObjectHeader reads and verifies an object's header, leaving r at the
// first byte of the payload.
func ReadObjectHeader(r io.Reader) (*ObjectManifest, error) {
	fixed := make([]byte, objectHeaderFixed)
	if _, err := io.ReadFull(r, fixed); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadObjectHeader, err)
	}
	if string(fixed[:len(ObjectMagic)]) != ObjectMagic {
		return nil, fmt.Errorf("%w: it does not begin with %s", ErrBadObjectHeader, ObjectMagic)
	}
	n := binary.BigEndian.Uint32(fixed[len(ObjectMagic) : len(ObjectMagic)+4])
	if n == 0 || n > MaxObjectManifest {
		return nil, fmt.Errorf("%w: the manifest length is %d bytes", ErrBadObjectHeader, n)
	}
	raw := make([]byte, n)
	if _, err := io.ReadFull(r, raw); err != nil {
		return nil, fmt.Errorf("%w: the manifest is truncated: %v", ErrBadObjectHeader, err)
	}
	sum := sha256.Sum256(raw)
	want := fixed[len(ObjectMagic)+4:]
	if subtle.ConstantTimeCompare(sum[:], want) != 1 {
		return nil, fmt.Errorf("%w: the manifest is not the bytes its digest describes", ErrBadObjectHeader)
	}
	var m ObjectManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadObjectHeader, err)
	}
	return &m, nil
}

// IsBundledObject reports whether r begins with the object header, and returns
// a reader that still yields the whole stream, header included.
func IsBundledObject(r io.Reader) (bool, io.Reader) {
	br := bufio.NewReaderSize(r, len(ObjectMagic))
	head, _ := br.Peek(len(ObjectMagic))
	return string(head) == ObjectMagic, br
}

// ObjectHeaderLen is the fixed size before the manifest JSON, useful to read
// only a header from a remote object.
func ObjectHeaderLen() int64 { return int64(objectHeaderFixed) }

// digestWriter counts and hashes what passes through it, without holding any
// of it. Compression goes over this writer, so the digest is of the
// uncompressed archive rather than of the bytes that happen to be written
// first.
type digestWriter struct {
	w io.Writer
	h hash.Hash
	n int64
}

func newDigestWriter(w io.Writer) *digestWriter {
	return &digestWriter{w: w, h: sha256.New()}
}

func (d *digestWriter) Write(p []byte) (int, error) {
	n, err := d.w.Write(p)
	if n > 0 {
		d.h.Write(p[:n])
		d.n += int64(n)
	}
	return n, err
}

func (d *digestWriter) hex() string {
	return fmt.Sprintf("%x", d.h.Sum(nil))
}
