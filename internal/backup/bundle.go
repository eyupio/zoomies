package backup

import (
	"archive/zip"
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// A backup downloaded for a person is a zip of the two files a local copy
// consists of: the sealed payload and the manifest that describes it. A zip is
// what every desktop opens without being told what it is, and it keeps the
// manifest beside the payload instead of making one opaque file.

// writeEncryptedArchive seals a backup's archive under passphrase. It returns
// the plaintext archive's size and digest, measured before compression.
func writeEncryptedArchive(w io.Writer, entry *Entry, passphrase string, compress bool) (int64, string, error) {
	if strings.TrimSpace(passphrase) == "" {
		return 0, "", errors.New("backup: a passphrase is needed to encrypt")
	}
	enc, err := NewEncryptor(w, passphrase)
	if err != nil {
		return 0, "", err
	}
	size, digest, err := writeArchive(enc, entry, compress)
	if err != nil {
		return 0, "", err
	}
	if err := enc.Close(); err != nil {
		return 0, "", err
	}
	return size, digest, nil
}

// BundleName is the file a person downloads for a backup.
func BundleName(id string) string { return id + ".zip" }

// WriteBundle writes a zip holding NAME.sealed and NAME.json. The sealed file
// is the archive under passphrase, which is the same stream a stored object
// carries. The caller must give a passphrase: a download is the whole fleet,
// and never leaves this host unsealed.
func WriteBundle(w io.Writer, entry *Entry, passphrase string, compress bool) error {
	if strings.TrimSpace(passphrase) == "" {
		return errors.New("backup: the backup has no key or passphrase to seal a download with")
	}
	spool, err := os.CreateTemp(filepath.Dir(entry.Dir), ".backup-bundle-*")
	if err != nil {
		return err
	}
	defer func() {
		spool.Close()
		_ = os.Remove(spool.Name())
	}()
	if _, _, err := writeEncryptedArchive(spool, entry, passphrase, compress); err != nil {
		return err
	}
	size, err := spool.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	if _, err := spool.Seek(0, io.SeekStart); err != nil {
		return err
	}
	manifest := []byte("{}\n")
	if entry.Manifest != nil {
		manifest, err = marshalManifest(entry.Manifest)
		if err != nil {
			return err
		}
	}

	zw := zip.NewWriter(w)
	sealed, err := zw.CreateHeader(&zip.FileHeader{Name: entry.ID + ".sealed", Method: zip.Store})
	if err != nil {
		return err
	}
	if _, err := io.Copy(sealed, io.LimitReader(spool, size)); err != nil {
		return err
	}
	described, err := zw.CreateHeader(&zip.FileHeader{Name: entry.ID + ".json", Method: zip.Deflate})
	if err != nil {
		return err
	}
	if _, err := described.Write(manifest); err != nil {
		return err
	}
	return zw.Close()
}

// UnpackBundle reads a downloaded zip back into an ordinary backup. The sealed
// member is opened with passphrase when one is given, otherwise with the key
// this host holds; nothing is written until the archive has been verified.
func UnpackBundle(ctx context.Context, root string, r io.ReaderAt, size int64, passphrase, innerPassphrase string, opts UnpackOptions) (*Entry, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("backup: this is not a downloaded backup zip: %w", err)
	}
	var sealed, described *zip.File
	for _, f := range zr.File {
		name := filepath.Base(f.Name)
		switch {
		case strings.HasSuffix(name, ".sealed"):
			sealed = f
		case strings.HasSuffix(name, ".json"):
			described = f
		}
	}
	if sealed == nil || described == nil {
		return nil, errors.New("backup: a downloaded backup zip holds NAME.sealed and NAME.json")
	}
	mr, err := described.Open()
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(mr, MaxObjectManifest+1))
	mr.Close()
	if err != nil {
		return nil, err
	}
	if len(raw) > MaxObjectManifest {
		return nil, ErrManifestTooLarge
	}
	var m Manifest
	if err := jsonUnmarshalManifest(raw, &m); err != nil {
		return nil, err
	}

	body, err := sealed.Open()
	if err != nil {
		return nil, err
	}
	defer body.Close()
	br := bufio.NewReader(body)
	encrypted, _ := IsEncrypted(br)
	var src io.Reader = br
	if encrypted {
		key := strings.TrimSpace(passphrase)
		if key == "" {
			key = strings.TrimSpace(innerPassphrase)
		}
		if key == "" {
			return nil, errors.New("backup: this download is encrypted and neither a passphrase nor this host's key was given")
		}
		src, err = NewDecryptor(br, key)
		if err != nil {
			return nil, err
		}
	}
	return Unpack(ctx, root, src, opts)
}

// IsBundle reports whether r begins with a zip header, and returns a reader
// that still yields the whole stream.
func IsBundle(r io.Reader) (bool, io.Reader) {
	br := bufio.NewReaderSize(r, 4)
	head, _ := br.Peek(4)
	return string(head) == "PK\x03\x04", br
}

func jsonUnmarshalManifest(raw []byte, m *Manifest) error {
	if err := json.Unmarshal(raw, m); err != nil {
		return fmt.Errorf("backup: %s is not a manifest: %w", ManifestName, err)
	}
	return nil
}
