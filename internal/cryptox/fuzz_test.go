package cryptox

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"testing"
)

func FuzzParseKey(f *testing.F) {
	raw := bytes.Repeat([]byte{0x42}, KeyLen)
	for _, s := range []string{"", "invalid", hex.EncodeToString(raw), base64.StdEncoding.EncodeToString(raw), base64.RawURLEncoding.EncodeToString(raw)} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, encoded string) {
		key, err := ParseKey(encoded)
		if err != nil {
			return
		}
		if key == nil || len(key.raw) != KeyLen {
			t.Fatal("accepted an invalid key")
		}
		roundTrip, err := ParseKey(key.Encode())
		if err != nil || !bytes.Equal(key.raw, roundTrip.raw) {
			t.Fatal("key encoding did not round-trip")
		}
	})
}

func FuzzSealOpen(f *testing.F) {
	f.Add([]byte("secret"))
	f.Add([]byte{})
	f.Add([]byte{0, 255, 0})
	f.Fuzz(func(t *testing.T, plaintext []byte) {
		key, err := NewKey(bytes.Repeat([]byte{0x42}, KeyLen))
		if err != nil {
			t.Fatal(err)
		}
		sealed, err := key.Seal(plaintext)
		if err != nil {
			t.Fatal(err)
		}
		opened, err := key.Open(sealed)
		if err != nil || !bytes.Equal(opened, plaintext) {
			t.Fatal("encryption did not round-trip")
		}
		if len(sealed) > 0 {
			sealed[len(sealed)-1] ^= 1
			if _, err := key.Open(sealed); err == nil {
				t.Fatal("accepted tampered ciphertext")
			}
		}
		// Arbitrary ciphertext, including truncated nonces, must not panic.
		_, _ = key.Open(plaintext)
	})
}
