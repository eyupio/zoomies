package store

import (
	"context"
	"errors"
	"testing"
)

// A destination has to survive the round trip whole: the offsite pass reads
// these rows and signs requests with what comes back, so a field that silently
// did not persist is a bucket nobody can write to -- or, worse, the wrong one.
func TestABackupRemoteComesBackAsItWasWritten(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	pathStyle := true

	r := &BackupRemote{
		Name: "offsite", Endpoint: "https://s3.eu-west-2.amazonaws.com", Region: "eu-west-2",
		Bucket: "acme-zoomies", Prefix: "prod", AccessKeyID: "AKIAEXAMPLE",
		PathStyle: &pathStyle, Keep: 30, Enabled: true,
	}
	if err := s.CreateBackupRemote(ctx, r); err != nil {
		t.Fatalf("CreateBackupRemote: %v", err)
	}
	if !HasPrefix(r.ID, PrefixBackupRemote) {
		t.Errorf("the id %q does not say what it is", r.ID)
	}

	got, err := s.GetBackupRemoteByName(ctx, "offsite")
	if err != nil {
		t.Fatalf("GetBackupRemoteByName: %v", err)
	}
	if got.Endpoint != r.Endpoint || got.Region != "eu-west-2" || got.Bucket != "acme-zoomies" ||
		got.Prefix != "prod" || got.AccessKeyID != "AKIAEXAMPLE" || got.Keep != 30 || !got.Enabled {
		t.Fatalf("the destination came back as %+v", got)
	}
	if got.PathStyle == nil || !*got.PathStyle {
		t.Errorf("path style came back as %v; an explicit choice must not read as unset", got.PathStyle)
	}

	// Unset is a third answer, not false: it means "choose from the endpoint",
	// which is what every deployment that has never heard of the setting wants.
	r.PathStyle = nil
	if err := s.UpdateBackupRemote(ctx, r); err != nil {
		t.Fatalf("UpdateBackupRemote: %v", err)
	}
	if got, err = s.GetBackupRemote(ctx, r.ID); err != nil {
		t.Fatalf("GetBackupRemote: %v", err)
	}
	if got.PathStyle != nil {
		t.Errorf("path style came back as %v after being cleared", *got.PathStyle)
	}
}

// The secrets go in by their own statement, and the form that carries the rest
// of the row must not be able to wipe them: an operator editing the retention
// on a page they opened yesterday is not rotating the key.
func TestWritingABackupRemotesFormLeavesItsSecretsAlone(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	r := &BackupRemote{Name: "offsite", Endpoint: "http://minio:9000", Bucket: "backups", Enabled: true}
	if err := s.CreateBackupRemote(ctx, r); err != nil {
		t.Fatalf("CreateBackupRemote: %v", err)
	}
	if err := s.SetBackupRemoteSecrets(ctx, r.ID, []byte("sealed-secret"), []byte("sealed-passphrase")); err != nil {
		t.Fatalf("SetBackupRemoteSecrets: %v", err)
	}

	r.Keep = 7
	if err := s.UpdateBackupRemote(ctx, r); err != nil {
		t.Fatalf("UpdateBackupRemote: %v", err)
	}
	got, err := s.GetBackupRemote(ctx, r.ID)
	if err != nil {
		t.Fatalf("GetBackupRemote: %v", err)
	}
	if string(got.SecretKeyEnc) != "sealed-secret" || string(got.PassphraseEnc) != "sealed-passphrase" {
		t.Fatalf("the form wiped the secrets: %q / %q", got.SecretKeyEnc, got.PassphraseEnc)
	}
	if got.Keep != 7 {
		t.Errorf("keep came back as %d", got.Keep)
	}

	// And clearing is explicit, which is how a passphrase is removed from a
	// destination that should send the plain archive.
	if err := s.SetBackupRemoteSecrets(ctx, r.ID, got.SecretKeyEnc, nil); err != nil {
		t.Fatalf("clearing the passphrase: %v", err)
	}
	if got, err = s.GetBackupRemote(ctx, r.ID); err != nil {
		t.Fatalf("GetBackupRemote: %v", err)
	}
	if len(got.PassphraseEnc) != 0 || string(got.SecretKeyEnc) != "sealed-secret" {
		t.Errorf("clearing one secret disturbed the other: %q / %q", got.SecretKeyEnc, got.PassphraseEnc)
	}
}

// The name is the address -- the API route, the log line, the problems drawer
// entry -- so two destinations may not share one. The database says so rather
// than the handler, because a second writer would eventually forget.
func TestTwoBackupRemotesCannotShareAName(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	first := &BackupRemote{Name: "offsite", Endpoint: "http://minio:9000", Bucket: "a", Enabled: true}
	if err := s.CreateBackupRemote(ctx, first); err != nil {
		t.Fatalf("CreateBackupRemote: %v", err)
	}
	second := &BackupRemote{Name: "offsite", Endpoint: "http://minio:9000", Bucket: "b", Enabled: true}
	if err := s.CreateBackupRemote(ctx, second); !errors.Is(err, ErrConflict) {
		t.Fatalf("the second destination called offsite was accepted: %v", err)
	}

	if err := s.DeleteBackupRemote(ctx, first.ID); err != nil {
		t.Fatalf("DeleteBackupRemote: %v", err)
	}
	if _, err := s.GetBackupRemote(ctx, first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the deleted destination is still there: %v", err)
	}
	if err := s.DeleteBackupRemote(ctx, first.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleting it twice answered %v", err)
	}
	remotes, err := s.ListBackupRemotes(ctx)
	if err != nil || len(remotes) != 0 {
		t.Errorf("the list holds %d destinations, %v", len(remotes), err)
	}
}
