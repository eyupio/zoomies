package backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/cryptox"
	"github.com/eyupio/zoomies/internal/store"
)

// fakeStore is an object store that lasts as long as the test.
func fakeStore(t *testing.T) *FakeS3 {
	t.Helper()
	f := NewFakeS3("backups")
	t.Cleanup(f.Close)
	return f
}

// remoteFor is a configured remote pointed at the fake, with whatever the test
// wants changed about it.
func remoteFor(t *testing.T, f *FakeS3, adjust func(*config.BackupRemote)) *Remote {
	t.Helper()
	cfg := config.BackupRemote{
		Name: "offsite", Endpoint: f.Endpoint(), Bucket: f.Bucket(), Prefix: "fleet",
		AccessKeyID: "AKIAEXAMPLE", SecretAccessKey: "secret",
	}
	if adjust != nil {
		adjust(&cfg)
	}
	key, err := cryptox.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	remote, err := NewRemoteWithKey(cfg, nil, key)
	if err != nil {
		t.Fatalf("NewRemoteWithKey: %v", err)
	}
	return remote
}

// takeOne is one backup on disk, which is what every remote operation is about.
func takeOne(t *testing.T, at time.Time) (*config.Config, *Entry) {
	t.Helper()
	cfg, st, _ := host(t)
	entry, err := Take(context.Background(), st, TakeOptions{
		Config: cfg, Source: SourceScheduled, TakenBy: "the schedule",
		Now: func() time.Time { return at },
	})
	if err != nil {
		t.Fatalf("Take: %v", err)
	}
	return cfg, entry
}

// The whole point, end to end: a backup taken here is in the bucket, comes
// back out of it, and is an ordinary backup when it does -- because a copy
// nobody has ever restored is a copy nobody knows about.
func TestUploadingABackupAndFetchingItBackGivesTheSameBackup(t *testing.T) {
	f := fakeStore(t)
	remote := remoteFor(t, f, nil)
	_, entry := takeOne(t, time.Date(2026, 3, 1, 2, 0, 0, 0, time.UTC))

	copied, err := remote.Upload(context.Background(), entry)
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if copied.Key != "fleet/"+entry.ID+".gz.enc" {
		t.Errorf("stored at %q, which is not the prefix and name a restore would look for", copied.Key)
	}
	if copied.Bytes <= 0 {
		t.Errorf("the copy reports %d bytes", copied.Bytes)
	}

	held, err := remote.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(held) != 1 || held[0].ID != entry.ID {
		t.Fatalf("the bucket lists %+v, wanted the one backup %s", held, entry.ID)
	}
	if held[0].TakenAt.IsZero() {
		t.Error("a listing should be able to say when a backup was taken from its id alone")
	}

	// Somewhere else entirely, as a second fleet's directory would be.
	root := filepath.Join(t.TempDir(), "restored")
	fetched, err := remote.Fetch(context.Background(), root, entry.ID, FetchOptions{TakenBy: "alice"})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if fetched.ID != entry.ID {
		t.Errorf("fetched %s, wanted %s", fetched.ID, entry.ID)
	}
	if fetched.Source != SourceFetched {
		t.Errorf("the fetched copy says it came from %q; it came out of a bucket", fetched.Source)
	}
	if fetched.Manifest == nil || fetched.Manifest.Database.SHA256 != entry.Manifest.Database.SHA256 {
		t.Fatal("the fetched database is not the one that was uploaded")
	}

	v, err := Verify(context.Background(), root, fetched.ID)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !v.OK {
		t.Errorf("the copy that came back does not verify: %v", v.Problems)
	}
}

// A remote with a passphrase must put an encrypted file in the bucket, not a
// database with a different name: the file is on somebody else's disk, and
// whoever holds the bucket is not the person who is allowed to read the fleet.
func TestAnEncryptedRemoteUploadsSomethingThatDoesNotOpenWithoutThePassphrase(t *testing.T) {
	f := fakeStore(t)
	remote := remoteFor(t, f, func(r *config.BackupRemote) { r.Passphrase = "a long passphrase" })
	_, entry := takeOne(t, time.Date(2026, 3, 1, 2, 0, 0, 0, time.UTC))

	copied, err := remote.Upload(context.Background(), entry)
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if !strings.HasSuffix(copied.Key, ".gz.enc") {
		t.Errorf("an encrypted compressed copy is stored at %q, which does not say what it is", copied.Key)
	}

	// Somebody holding the bucket and neither key nor passphrase has the bytes
	// and nothing else.
	blind, err := NewRemote(config.BackupRemote{
		Name: "offsite", Endpoint: f.Endpoint(), Bucket: f.Bucket(), Prefix: "fleet",
		AccessKeyID: "AKIAEXAMPLE", SecretAccessKey: "secret",
	}, nil)
	if err != nil {
		t.Fatalf("NewRemote: %v", err)
	}
	if _, err := blind.Fetch(context.Background(), t.TempDir(), entry.ID, FetchOptions{}); err == nil {
		t.Error("a remote with no key and no passphrase fetched an encrypted copy")
	}
	if _, err := remote.Fetch(context.Background(), t.TempDir(), entry.ID, FetchOptions{Passphrase: "the wrong one"}); err == nil {
		t.Error("the wrong passphrase opened the archive")
	}

	root := t.TempDir()
	fetched, err := remote.Fetch(context.Background(), root, entry.ID, FetchOptions{})
	if err != nil {
		t.Fatalf("Fetch with the remote's own passphrase: %v", err)
	}
	if fetched.Manifest == nil || fetched.Manifest.Database.SHA256 != entry.Manifest.Database.SHA256 {
		t.Error("what came back is not what went in")
	}
}

// Retention on the remote keeps the newest and nothing else, and it only ever
// removes keys this package writes -- a bucket is usually shared.
func TestRemoteRetentionKeepsTheNewestAndLeavesStrangersAlone(t *testing.T) {
	f := fakeStore(t)
	remote := remoteFor(t, f, nil)
	f.Put("fleet/somebody-elses-backup.tar.gz", []byte("not ours"))

	cfg, st, _ := host(t)
	var ids []string
	for i := range 4 {
		at := time.Date(2026, 3, 1, 2, i, 0, 0, time.UTC)
		entry, err := Take(context.Background(), st, TakeOptions{Config: cfg, Source: SourceScheduled, Now: func() time.Time { return at }})
		if err != nil {
			t.Fatalf("Take: %v", err)
		}
		if _, err := remote.Upload(context.Background(), entry); err != nil {
			t.Fatalf("Upload: %v", err)
		}
		ids = append(ids, entry.ID)
	}

	removed, err := remote.Prune(context.Background(), 2)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if len(removed) != 2 {
		t.Fatalf("removed %v, wanted the two oldest", removed)
	}
	held, err := remote.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(held) != 2 || held[0].ID != ids[3] || held[1].ID != ids[2] {
		t.Errorf("the bucket holds %+v, wanted the newest two", held)
	}
	for _, key := range f.Keys() {
		if key == "fleet/somebody-elses-backup.tar.gz" {
			return
		}
	}
	t.Error("retention deleted an object this package did not write")
}

// A remote that was unreachable for two nights is two backups behind, and the
// next pass has to send both. Missing is what decides that, and it is a pure
// function so the rule is testable without a bucket.
func TestMissingSendsTheBacklogButNeverMoreThanTheRemoteKeeps(t *testing.T) {
	entries := []Entry{
		{ID: "zoomies-20260304-020000", Source: SourceScheduled},
		{ID: "zoomies-20260303-020000", Source: SourceScheduled},
		{ID: "zoomies-20260302-020000", Source: SourceScheduled},
		{ID: "zoomies-20260301-020000", Source: SourceScheduled},
	}
	held := []Copy{{ID: "zoomies-20260301-020000"}}

	missing := Missing(entries, held, 0)
	if len(missing) != 3 {
		t.Fatalf("with no ceiling, %d of the backlog would be sent; wanted 3", len(missing))
	}
	if missing[0].ID != "zoomies-20260302-020000" {
		t.Errorf("the backlog starts at %s; it is sent oldest first, so an interrupted catch-up leaves the bucket holding a contiguous run rather than a gap", missing[0].ID)
	}

	// A remote keeping two is never sent the third-oldest only to delete it.
	missing = Missing(entries, held, 2)
	if len(missing) != 2 || missing[0].ID != "zoomies-20260303-020000" {
		t.Errorf("with keep=2 the backlog is %+v; wanted only the newest two", missing)
	}

	// The store's pre-migration copies have no manifest and are the fleet's
	// own scaffolding, not its backups.
	pre := append([]Entry{{ID: "zoomies-20260305-020000", Source: SourcePreMigration}}, entries...)
	for _, e := range Missing(pre, nil, 0) {
		if e.Source == SourcePreMigration {
			t.Error("a pre-migration copy was queued for a bucket")
		}
	}

	// A backup whose database has gone is not something to upload.
	broken := []Entry{{ID: "zoomies-20260306-020000", Source: SourceScheduled, Problem: "the database is missing"}}
	if got := Missing(broken, nil, 0); len(got) != 0 {
		t.Errorf("a broken backup was queued for a bucket: %+v", got)
	}
}

// What a remote refuses has to arrive as a sentence an operator can act on:
// the fix for a wrong secret and the fix for a missing bucket are different
// jobs, and "403" is neither.
func TestARefusedRequestSaysWhatToDoAboutIt(t *testing.T) {
	f := fakeStore(t)
	remote := remoteFor(t, f, nil)

	for _, tc := range []struct {
		name   string
		status int
		code   string
		want   string
	}{
		{"a bucket that is not there", 404, "NoSuchBucket", "never creates one"},
		{"a secret that is wrong", 403, "SignatureDoesNotMatch", "secret access key is wrong"},
		{"a policy that says no", 403, "AccessDenied", "s3:PutObject"},
		{"a clock that has drifted", 403, "RequestTimeTooSkewed", "clock"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f.BreakWith(tc.status, tc.code, "refused")
			defer f.Mend()
			err := remote.Check(context.Background())
			if err == nil {
				t.Fatal("the check passed against a service that refused it")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("the error reads %q, which does not tell the operator about %q", err, tc.want)
			}
		})
	}
}

// A remote holds one fleet's copies under a prefix; a bucket shared with
// another fleet must not make one appear in the other's list.
func TestAListingIsScopedToTheRemotesOwnPrefix(t *testing.T) {
	f := fakeStore(t)
	ours := remoteFor(t, f, func(r *config.BackupRemote) { r.Prefix = "prod" })
	theirs := remoteFor(t, f, func(r *config.BackupRemote) { r.Name = "other"; r.Prefix = "staging" })

	cfg, st, _ := host(t)
	entry, err := Take(context.Background(), st, TakeOptions{Config: cfg, Source: SourceScheduled})
	if err != nil {
		t.Fatalf("Take: %v", err)
	}
	if _, err := theirs.Upload(context.Background(), entry); err != nil {
		t.Fatalf("Upload: %v", err)
	}

	held, err := ours.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(held) != 0 {
		t.Errorf("the prod remote lists %+v, which is staging's", held)
	}
}

// A raw string prefix is not a directory: "prod" also matches "prod-eu/...",
// "zoomies" matches "zoomies/prod/...", and an empty prefix matches the whole
// bucket. Retention that listed by string prefix would delete another fleet's
// offsite backups without a word, which is the one failure it must not have.
func TestAListingNeverReachesIntoASiblingOrNestedPrefix(t *testing.T) {
	for _, tc := range []struct{ ours, theirs string }{
		{"prod", "prod-eu"},
		{"zoomies", "zoomies/prod"},
		{"", "staging"},
	} {
		t.Run(tc.ours+" against "+tc.theirs, func(t *testing.T) {
			f := fakeStore(t)
			ours := remoteFor(t, f, func(r *config.BackupRemote) { r.Prefix = tc.ours })
			theirs := remoteFor(t, f, func(r *config.BackupRemote) { r.Name = "other"; r.Prefix = tc.theirs })
			ctx := context.Background()

			base := time.Date(2026, 1, 1, 2, 0, 0, 0, time.UTC)
			// Theirs are older than ours, so a listing that merged the two would
			// hand them to retention first.
			for i := 0; i < 2; i++ {
				_, entry := takeOne(t, base.Add(time.Duration(i)*time.Hour))
				if _, err := theirs.Upload(ctx, entry); err != nil {
					t.Fatalf("Upload: %v", err)
				}
			}
			_, entry := takeOne(t, base.Add(48*time.Hour))
			if _, err := ours.Upload(ctx, entry); err != nil {
				t.Fatalf("Upload: %v", err)
			}

			held, err := ours.List(ctx)
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if len(held) != 1 || held[0].ID != entry.ID {
				t.Fatalf("the %q remote lists %+v, which is not just its own copy", tc.ours, held)
			}

			removed, err := ours.Prune(ctx, 1)
			if err != nil {
				t.Fatalf("Prune: %v", err)
			}
			if len(removed) != 0 {
				t.Errorf("pruning the %q remote removed %v, which are %q's", tc.ours, removed, tc.theirs)
			}
			kept, err := theirs.List(ctx)
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if len(kept) != 2 {
				t.Errorf("the %q remote holds %d copies after the other's retention pass, wanted 2", tc.theirs, len(kept))
			}
		})
	}
}

// The endpoint decides the request style, because the alternative is a setting
// that silently disagrees with the endpoint: a bucket in the hostname needs
// DNS nobody arranges for a MinIO on a private network.
func TestPathStyleIsChosenByTheEndpointUnlessItIsSaid(t *testing.T) {
	yes, no := true, false
	for _, tc := range []struct {
		endpoint  string
		pathStyle *bool
		want      string
	}{
		{"https://s3.eu-west-2.amazonaws.com", nil, "https://backups.s3.eu-west-2.amazonaws.com/k"},
		{"http://minio:9000", nil, "http://minio:9000/backups/k"},
		{"https://s3.eu-west-2.amazonaws.com", &yes, "https://s3.eu-west-2.amazonaws.com/backups/k"},
		{"http://minio:9000", &no, "http://backups.minio:9000/k"},
	} {
		client, err := newS3Client(tc.endpoint, "", "backups", "id", "secret", tc.pathStyle, nil)
		if err != nil {
			t.Fatalf("newS3Client(%s): %v", tc.endpoint, err)
		}
		if got := client.url("k").String(); got != tc.want {
			t.Errorf("%s addresses a key as %s, wanted %s", tc.endpoint, got, tc.want)
		}
	}
}

// The signature is the one part of this with an external definition, so it is
// checked against the example AWS documents it with rather than against
// itself.
func TestTheCanonicalRequestIsEncodedTheWayTheSignatureDefinesIt(t *testing.T) {
	if got := uriEncode("a b/c~d.e", false); got != "a%20b/c~d.e" {
		t.Errorf("a path encodes as %q; a space is %%20, a tilde and a dot are themselves, and a slash stays", got)
	}
	if got := uriEncode("a b/c", true); got != "a%20b%2Fc" {
		t.Errorf("a query value encodes as %q; outside a path a slash is encoded too", got)
	}
	if emptyPayloadSHA256 != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Error("the empty payload digest is not the digest of no bytes, so every unsigned-body request would be refused")
	}
}

// The merge rule has to hold for every caller -- the controller's pass, the
// API and `zoomies restore --from-remote` on a host with no controller -- so
// it is tested where it lives rather than three times over.
func TestTheFileWinsAndAStoredSecretThatWillNotOpenIsReported(t *testing.T) {
	f := fakeStore(t)
	cfg := config.Default()
	cfg.Backup.Remotes = []config.BackupRemote{{
		Name: "offsite", Endpoint: f.Endpoint(), Bucket: f.Bucket(),
		AccessKeyID: "AKIAEXAMPLE", SecretAccessKey: "from-the-file",
	}}
	key, err := cryptox.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	sealed, err := key.SealString("from-the-database")
	if err != nil {
		t.Fatalf("SealString: %v", err)
	}
	stranger, err := cryptox.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	strange, err := stranger.SealString("somebody else's")
	if err != nil {
		t.Fatalf("SealString: %v", err)
	}

	rows := fakeRemotes{
		{ID: "bkr_1", Name: "offsite", Endpoint: "https://elsewhere.example.com", Bucket: "other",
			AccessKeyID: "a", SecretKeyEnc: sealed, Enabled: true},
		{ID: "bkr_2", Name: "second", Endpoint: "https://s3.example.com", Bucket: "second",
			AccessKeyID: "b", SecretKeyEnc: sealed, Enabled: true},
		{ID: "bkr_3", Name: "strange", Endpoint: "https://s3.example.com", Bucket: "third",
			AccessKeyID: "c", SecretKeyEnc: strange, Enabled: true},
	}

	resolved, err := ResolveRemotes(context.Background(), cfg, rows, key)
	if err != nil {
		t.Fatalf("ResolveRemotes: %v", err)
	}
	if len(resolved) != 4 {
		t.Fatalf("resolved %d destinations, wanted the file's and all three rows", len(resolved))
	}
	if resolved[0].Origin != OriginFile || resolved[0].Remote.SecretAccessKey != "from-the-file" {
		t.Errorf("the file's destination reads %+v", resolved[0])
	}
	if !resolved[1].Shadowed || resolved[1].Usable() {
		t.Errorf("the stored destination of the same name is %+v; the file has the last word", resolved[1])
	}
	if resolved[2].Remote.SecretAccessKey != "from-the-database" || !resolved[2].Usable() {
		t.Errorf("a stored destination with its own name reads %+v", resolved[2])
	}
	if resolved[3].Problem == "" || resolved[3].Usable() {
		t.Errorf("a stored destination sealed with another key reads %+v; it must not be tried", resolved[3])
	}

	// And the lookup every route uses agrees with the list.
	found, err := FindResolvedRemote(context.Background(), cfg, rows, key, "offsite")
	if err != nil || found.Origin != OriginFile {
		t.Errorf("looking up offsite gave %+v, %v", found, err)
	}
	if _, err := FindResolvedRemote(context.Background(), cfg, rows, key, "strange"); err == nil {
		t.Error("a destination whose secret will not open was handed out")
	}
	if _, err := FindResolvedRemote(context.Background(), cfg, rows, key, "nowhere"); !errors.Is(err, ErrNoRemote) {
		t.Errorf("an unknown name gave %v", err)
	}

	// A fleet with no database at all still has the file's destinations,
	// which is the case `zoomies restore --from-remote` exists for.
	resolved, err = ResolveRemotes(context.Background(), cfg, nil, key)
	if err != nil || len(resolved) != 1 || resolved[0].Origin != OriginFile {
		t.Errorf("with no database the fleet resolved %+v, %v", resolved, err)
	}
}

// fakeRemotes is a stored-destination list without a database behind it.
type fakeRemotes []store.BackupRemote

func (f fakeRemotes) ListBackupRemotes(context.Context) ([]*store.BackupRemote, error) {
	out := make([]*store.BackupRemote, len(f))
	for i := range f {
		row := f[i]
		out[i] = &row
	}
	return out, nil
}

// A new copy is one object, and its name says whether it was compressed before
// it was encrypted. A sidecar manifest would be a second thing that can go
// missing from somebody else's bucket.
func TestANewBackupIsOneObjectNamedForItsCompression(t *testing.T) {
	f := fakeStore(t)
	ctx := context.Background()
	remote := remoteFor(t, f, nil)

	_, first := takeOne(t, time.Date(2026, 3, 2, 2, 0, 0, 0, time.UTC))
	compressed, err := remote.Upload(ctx, first)
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if compressed.Key != "fleet/"+first.ID+".gz.enc" {
		t.Errorf("a compressed copy is stored at %q", compressed.Key)
	}
	if f.Count() != 1 {
		t.Fatalf("one backup left %d objects in the bucket", f.Count())
	}
	body, ok := f.Body(compressed.Key)
	if !ok || !bytes.HasPrefix(body, []byte(ObjectMagic)) {
		t.Fatalf("the one object does not begin with %s", ObjectMagic)
	}

	no := false
	plain := remoteFor(t, f, func(r *config.BackupRemote) { r.Compress = &no })
	_, second := takeOne(t, time.Date(2026, 3, 2, 2, 1, 0, 0, time.UTC))
	uncompressed, err := plain.Upload(ctx, second)
	if err != nil {
		t.Fatalf("Upload uncompressed: %v", err)
	}
	if uncompressed.Key != "fleet/"+second.ID+".enc" {
		t.Errorf("an uncompressed copy is stored at %q", uncompressed.Key)
	}
	root := t.TempDir()
	fetched, err := plain.Fetch(ctx, root, second.ID, FetchOptions{})
	if err != nil {
		t.Fatalf("Fetch uncompressed: %v", err)
	}
	if v, err := Verify(ctx, root, fetched.ID); err != nil || !v.OK {
		t.Errorf("an uncompressed copy does not verify: %+v, %v", v, err)
	}
}

// The header is the only thing a reader trusts before it sees the payload, so
// its own digest has to be checked and a changed byte refused.
func TestTheObjectHeaderRoundTripsAndRefusesCorruption(t *testing.T) {
	m := &ObjectManifest{
		ManifestVersion: 1,
		Target:          "zoomies",
		TakenAt:         time.Date(2026, 9, 12, 3, 0, 4, 0, time.UTC),
		PlaintextSize:   1234,
		PlaintextSHA256: strings.Repeat("a", 64),
		PayloadSize:     5678,
		PayloadSHA256:   strings.Repeat("b", 64),
		Compression:     "gzip",
		KeyFingerprint:  "the-key",
		Version:         "1.0",
	}
	var buf bytes.Buffer
	if err := WriteObjectHeader(&buf, m); err != nil {
		t.Fatalf("WriteObjectHeader: %v", err)
	}
	raw := bytes.Clone(buf.Bytes())
	got, err := ReadObjectHeader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("ReadObjectHeader: %v", err)
	}
	if got.Target != m.Target || got.PayloadSHA256 != m.PayloadSHA256 || got.Compression != "gzip" {
		t.Fatalf("header round trip = %+v", got)
	}

	for name, breakIt := range map[string]func([]byte){
		"the magic":    func(b []byte) { b[0] ^= 0xff },
		"the length":   func(b []byte) { b[len(ObjectMagic)] = 0x7f },
		"the digest":   func(b []byte) { b[len(ObjectMagic)+4] ^= 0xff },
		"the manifest": func(b []byte) { b[len(ObjectMagic)+4+sha256.Size] ^= 0xff },
	} {
		t.Run(name, func(t *testing.T) {
			bad := bytes.Clone(raw)
			breakIt(bad)
			if _, err := ReadObjectHeader(bytes.NewReader(bad)); !errors.Is(err, ErrBadObjectHeader) {
				t.Fatalf("ReadObjectHeader = %v, want ErrBadObjectHeader", err)
			}
		})
	}
}

// A listing is a key listing. A bucket with a hundred copies must not cost a
// hundred downloads, which is why the manifest sits inside the object and the
// name carries enough to list it.
func TestListingUsesTheKeyListingAndNeverOpensACopy(t *testing.T) {
	f := fakeStore(t)
	ctx := context.Background()
	remote := remoteFor(t, f, nil)
	for i := range 3 {
		_, entry := (func() (*config.Config, *Entry) {
			cfg, st, _ := host(t)
			e, err := Take(ctx, st, TakeOptions{Config: cfg, Source: SourceScheduled,
				Now: func() time.Time { return time.Date(2026, 3, 3, 2, i, 0, 0, time.UTC) }})
			if err != nil {
				t.Fatalf("Take: %v", err)
			}
			return cfg, e
		})()
		if _, err := remote.Upload(ctx, entry); err != nil {
			t.Fatalf("Upload: %v", err)
		}
	}
	before := f.Gets()
	held, err := remote.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(held) != 3 {
		t.Fatalf("List found %d copies, wanted 3", len(held))
	}
	if after := f.Gets(); after != before {
		t.Errorf("listing opened %d object(s); it should use the key listing alone", after-before)
	}
}

// A destination passphrase goes over the already-encrypted payload, and the
// manifest records only what the outer layer is, never how to open it.
func TestADestinationPassphraseSealsAgainAndAManifestCarriesNoSecret(t *testing.T) {
	f := fakeStore(t)
	ctx := context.Background()
	const passphrase = "a destination passphrase that must never be written"
	remote := remoteFor(t, f, func(r *config.BackupRemote) {
		r.Passphrase = passphrase
		r.SecretAccessKey = "s3cr3t-access-value"
	})
	_, entry := takeOne(t, time.Date(2026, 3, 4, 2, 0, 0, 0, time.UTC))
	copied, err := remote.Upload(ctx, entry)
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	body, ok := f.Body(copied.Key)
	if !ok {
		t.Fatal("nothing landed")
	}
	om, err := ReadObjectHeader(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("ReadObjectHeader: %v", err)
	}
	if !om.PassphraseSealed || om.OuterSize == 0 || om.OuterSHA256 == "" {
		t.Fatalf("the manifest does not describe the outer seal: %+v", om)
	}
	for _, secret := range []string{passphrase, "AKIAEXAMPLE", "s3cr3t-access-value"} {
		if bytes.Contains(body, []byte(secret)) {
			t.Errorf("the object contains %q", secret)
		}
	}

	if _, err := remote.Fetch(ctx, t.TempDir(), entry.ID, FetchOptions{Passphrase: "the wrong passphrase"}); !errors.Is(err, ErrWrongPassphrase) {
		t.Errorf("the wrong passphrase gave %v, want ErrWrongPassphrase", err)
	}
	root := t.TempDir()
	fetched, err := remote.Fetch(ctx, root, entry.ID, FetchOptions{})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if v, err := Verify(ctx, root, fetched.ID); err != nil || !v.OK {
		t.Errorf("the fetched copy does not verify: %+v, %v", v, err)
	}
}

// The two-object layout an older controller wrote is still a backup: it lists,
// comes back, verifies and deletes as one copy.
func TestAnOldSealedAndManifestPairStillListsFetchesAndDeletes(t *testing.T) {
	f := fakeStore(t)
	ctx := context.Background()
	_, entry := takeOne(t, time.Date(2026, 3, 5, 2, 0, 0, 0, time.UTC))
	const passphrase = "an older destination passphrase"

	var archive bytes.Buffer
	enc, err := NewEncryptor(&archive, passphrase)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteArchive(enc, entry); err != nil {
		t.Fatal(err)
	}
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}
	manifest, err := marshalManifest(entry.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	f.Put("fleet/"+entry.ID+".sealed", archive.Bytes())
	f.Put("fleet/"+entry.ID+".json", manifest)

	remote := remoteFor(t, f, func(r *config.BackupRemote) {
		r.Passphrase = passphrase
		r.SecretAccessKey = "s3cr3t-access-value"
	})
	held, err := remote.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(held) != 1 || held[0].ID != entry.ID || !strings.HasSuffix(held[0].Key, ".sealed") {
		t.Fatalf("the old pair lists as %+v", held)
	}
	root := t.TempDir()
	fetched, err := remote.Fetch(ctx, root, entry.ID, FetchOptions{})
	if err != nil {
		t.Fatalf("Fetch old pair: %v", err)
	}
	if v, err := Verify(ctx, root, fetched.ID); err != nil || !v.OK {
		t.Errorf("an old copy does not verify: %+v, %v", v, err)
	}
	if err := remote.Delete(ctx, entry.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if f.Count() != 0 {
		t.Errorf("deleting an old copy left %d objects behind", f.Count())
	}
}

// A copy larger than the store's single-PUT limit goes in parts. A completion
// whose answer was lost is reconciled against the object, and a completion
// that failed leaves nothing advertised.
func TestAMultipartCopyCompletesEvenWhenTheAnswerIsLostOrFails(t *testing.T) {
	f := fakeStore(t)
	ctx := context.Background()
	remote := remoteFor(t, f, nil)
	remote.s3.multipartThreshold = 1024

	_, first := takeOne(t, time.Date(2026, 3, 6, 2, 0, 0, 0, time.UTC))
	copied, err := remote.Upload(ctx, first)
	if err != nil {
		t.Fatalf("multipart Upload: %v", err)
	}
	if body, ok := f.Body(copied.Key); !ok || !bytes.HasPrefix(body, []byte(ObjectMagic)) {
		t.Fatalf("the multipart object is not the one the header describes")
	}

	f.LoseCompletionAnswerOnce()
	_, second := takeOne(t, time.Date(2026, 3, 6, 2, 1, 0, 0, time.UTC))
	if _, err := remote.Upload(ctx, second); err != nil {
		t.Fatalf("an upload whose completion answer was lost was not reconciled: %v", err)
	}
	if _, ok := f.Body("fleet/" + second.ID + ".gz.enc"); !ok {
		t.Error("the reconciled upload is not in the bucket")
	}

	before := f.Count()
	f.FailCompletionOnce()
	_, third := takeOne(t, time.Date(2026, 3, 6, 2, 2, 0, 0, time.UTC))
	if _, err := remote.Upload(ctx, third); err == nil {
		t.Fatal("a failed completion was reported as a copy")
	}
	if f.Count() != before {
		t.Errorf("a failed completion left %d new objects", f.Count()-before)
	}
	held, err := remote.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range held {
		if c.ID == third.ID {
			t.Error("a copy that failed to complete is advertised by the listing")
		}
	}
}

// A caller that did not hand the remote a key can still upload a backup whose
// manifest names a readable key file: the copy is encrypted with that key and
// the header says which one, so the same host can bring it back.
func TestARemoteWithoutAKeySealsWithTheKeyTheBackupNames(t *testing.T) {
	f := fakeStore(t)
	ctx := context.Background()
	cfg := config.BackupRemote{
		Name: "offsite", Endpoint: f.Endpoint(), Bucket: f.Bucket(), Prefix: "fleet",
		AccessKeyID: "AKIAEXAMPLE", SecretAccessKey: "secret",
	}
	remote, err := NewRemote(cfg, nil)
	if err != nil {
		t.Fatalf("NewRemote: %v", err)
	}
	_, entry := takeOne(t, time.Date(2026, 3, 7, 2, 0, 0, 0, time.UTC))
	if _, err := remote.Upload(ctx, entry); err != nil {
		t.Fatalf("Upload with the key its manifest names: %v", err)
	}
	root := t.TempDir()
	if _, err := remote.Fetch(ctx, root, entry.ID, FetchOptions{}); err != nil {
		t.Fatalf("Fetch with the key its header names: %v", err)
	}
	if v, err := Verify(ctx, root, entry.ID); err != nil || !v.OK {
		t.Errorf("the copy does not verify: %+v, %v", v, err)
	}
}
