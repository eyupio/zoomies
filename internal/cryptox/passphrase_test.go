package cryptox

import (
	"errors"
	"testing"
)

// A secret sealed for another instance has to open with the passphrase alone,
// and with nothing else: not a wrong passphrase, not an altered copy.
func TestAPassphraseSealedValueOpensWithThePassphraseAndNothingElse(t *testing.T) {
	sealed, err := SealWithPassphrase("correct horse", []byte("-----BEGIN RSA PRIVATE KEY-----"))
	if err != nil {
		t.Fatalf("SealWithPassphrase: %v", err)
	}
	got, err := OpenWithPassphrase("correct horse", sealed)
	if err != nil || string(got) != "-----BEGIN RSA PRIVATE KEY-----" {
		t.Fatalf("OpenWithPassphrase = %q, %v", got, err)
	}
	if _, err := OpenWithPassphrase("battery staple", sealed); !errors.Is(err, ErrWrongPassphrase) {
		t.Errorf("a wrong passphrase gave %v, want ErrWrongPassphrase", err)
	}
	tampered := append([]byte(nil), sealed...)
	tampered[len(tampered)-1] ^= 1
	if _, err := OpenWithPassphrase("correct horse", tampered); !errors.Is(err, ErrWrongPassphrase) {
		t.Errorf("an altered value gave %v, want ErrWrongPassphrase", err)
	}
	again, _ := SealWithPassphrase("correct horse", []byte("-----BEGIN RSA PRIVATE KEY-----"))
	if string(again) == string(sealed) {
		t.Error("two seals of one value came out the same; the salt or nonce is not fresh")
	}
}

// A value the instance key sealed is the wrong kind of thing, and saying so is
// more use to the person holding it than "wrong passphrase".
func TestAnInstanceSealedValueIsRefusedAsTheWrongKind(t *testing.T) {
	k, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	instance, _ := k.Seal([]byte("x"))
	if _, err := OpenWithPassphrase("correct horse", instance); err == nil || errors.Is(err, ErrWrongPassphrase) {
		t.Errorf("an instance-sealed value gave %v, want a refusal naming the wrong kind", err)
	}
	if _, err := SealWithPassphrase("  ", []byte("x")); err == nil {
		t.Error("a blank passphrase sealed a value")
	}
}
