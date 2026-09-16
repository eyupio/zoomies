package config

import (
	"strings"
	"testing"
	"time"
)

// A compose deployment describes the bucket in the file and keeps the secret
// in the environment, which is the whole reason the environment half exists.
// Matching by name rather than by position is what makes that work when the
// file lists more than one destination.
func TestTheEnvironmentFillsInTheRemoteTheFileAlreadyDescribes(t *testing.T) {
	c := Default()
	c.Backup.Remotes = []BackupRemote{
		{Name: "minio", Endpoint: "http://minio:9000", Bucket: "first"},
		{Name: "offsite", Endpoint: "https://s3.eu-west-2.amazonaws.com", Bucket: "second"},
	}
	t.Setenv("ZOOMIES_BACKUP_REMOTE_NAME", "offsite")
	t.Setenv("ZOOMIES_BACKUP_REMOTE_SECRET_ACCESS_KEY", "the secret")
	t.Setenv("ZOOMIES_BACKUP_REMOTE_PASSPHRASE", "a long passphrase")

	if err := c.applyEnv(); err != nil {
		t.Fatalf("applyEnv: %v", err)
	}
	c.normalize()

	if got := c.Backup.Remotes[0].SecretAccessKey; got != "" {
		t.Errorf("the first remote gained %q; the variables named the second by name", got)
	}
	second := c.Backup.Remotes[1]
	if second.SecretAccessKey != "the secret" || !second.Encrypted() {
		t.Errorf("the named remote reads %+v", second)
	}
	if second.Bucket != "second" {
		t.Errorf("bucket = %q; the environment set no bucket and must not have cleared one", second.Bucket)
	}
}

// And a deployment with no file at all describes the whole destination in the
// environment, which is what a container without a mounted configuration has.
func TestARemoteCanBeDescribedEntirelyInTheEnvironment(t *testing.T) {
	c := Default()
	t.Setenv("ZOOMIES_BACKUP_REMOTE_ENDPOINT", "http://minio:9000")
	t.Setenv("ZOOMIES_BACKUP_REMOTE_BUCKET", "backups")
	t.Setenv("ZOOMIES_BACKUP_REMOTE_ACCESS_KEY_ID", "zoomies")
	t.Setenv("ZOOMIES_BACKUP_REMOTE_SECRET_ACCESS_KEY", "secret")
	t.Setenv("ZOOMIES_BACKUP_REMOTE_KEEP", "30")
	t.Setenv("ZOOMIES_BACKUP_REMOTE_2_ENDPOINT", "https://s3.eu-west-2.amazonaws.com")
	t.Setenv("ZOOMIES_BACKUP_REMOTE_2_BUCKET", "acme")
	t.Setenv("ZOOMIES_BACKUP_REMOTE_2_ACCESS_KEY_ID", "AKIA")
	t.Setenv("ZOOMIES_BACKUP_REMOTE_2_SECRET_ACCESS_KEY", "secret")

	if err := c.applyEnv(); err != nil {
		t.Fatalf("applyEnv: %v", err)
	}
	c.normalize()

	if len(c.Backup.Remotes) != 2 {
		t.Fatalf("the environment described %d remotes, wanted 2", len(c.Backup.Remotes))
	}
	// Named for them, because the name is what the page, the log and the API
	// address a destination by and nobody supplied one.
	if c.Backup.Remotes[0].Name != "offsite" || c.Backup.Remotes[1].Name != "offsite-2" {
		t.Errorf("unnamed remotes are called %q and %q", c.Backup.Remotes[0].Name, c.Backup.Remotes[1].Name)
	}
	if c.Backup.Remotes[0].Keep != 30 {
		t.Errorf("keep = %d, wanted the 30 the environment asked for", c.Backup.Remotes[0].Keep)
	}
	if enabled := c.EnabledBackupRemotes(); len(enabled) != 2 {
		t.Errorf("%d of the two remotes would be used", len(enabled))
	}
	if c.Source("backup.remotes") != SourceEnvironment {
		t.Error("a remote set from the environment does not say where it came from")
	}
}

// A number the environment cannot parse is refused by name rather than
// silently ignored: a remote with no retention is a bucket that grows forever,
// and a typo that reads as "keep everything" is one nobody notices.
func TestABadRemoteEnvironmentValueIsRefusedByName(t *testing.T) {
	c := Default()
	t.Setenv("ZOOMIES_BACKUP_REMOTE_ENDPOINT", "http://minio:9000")
	t.Setenv("ZOOMIES_BACKUP_REMOTE_KEEP", "lots")

	err := c.applyEnv()
	if err == nil {
		t.Fatal("a keep of \"lots\" was accepted")
	}
	if !strings.Contains(err.Error(), "ZOOMIES_BACKUP_REMOTE_KEEP") {
		t.Errorf("the error reads %q, which does not name the variable the operator wrote", err)
	}
}

// The two findings that are not errors are the point of the validator here: a
// bucket that can be read by whoever owns it, and credentials crossing a
// network in the clear, are both things a fleet may choose and neither is
// allowed to be silent.
func TestAnOffsiteBackupSaysWhatItIsGivingAway(t *testing.T) {
	base := func() *Config {
		c := Default()
		c.Backup.Interval = 24 * time.Hour
		c.Backup.Remotes = []BackupRemote{{
			Name: "offsite", Endpoint: "https://s3.eu-west-2.amazonaws.com", Bucket: "acme",
			AccessKeyID: "AKIA", SecretAccessKey: "secret",
		}}
		c.normalize()
		return c
	}

	c := base()
	if !hasCode(c.Validate(), "backup.remote_plaintext") {
		t.Error("a remote with no passphrase is sent the whole fleet in the clear and nothing says so")
	}
	if hasCode(c.Validate(), NoRemoteFinding) {
		t.Error("a fleet with a remote is told it has none")
	}

	// And the fleet that has none is told, under the code the settings API
	// drops once a destination stored in the database has answered it. The
	// constant and the finding are spelled separately, so this is what holds
	// them together.
	bare := Default()
	bare.Backup.Interval = 24 * time.Hour
	if !hasCode(bare.Validate(), NoRemoteFinding) {
		t.Error("a fleet taking backups that never leave the host is not told")
	}

	c = base()
	c.Backup.Remotes[0].Passphrase = "a long random passphrase"
	if f := c.Validate(); hasCode(f, "backup.remote_plaintext") || hasCode(f, "backup.remote_passphrase_short") {
		t.Error("a remote with a real passphrase is still being warned about")
	}

	c = base()
	c.Backup.Remotes[0].Passphrase = "short"
	if !hasCode(c.Validate(), "backup.remote_passphrase_short") {
		t.Error("a five-character passphrase passed without comment")
	}

	c = base()
	c.Backup.Remotes[0].Endpoint = "http://s3.example.com"
	if !hasCode(c.Validate(), "backup.remote_insecure") {
		t.Error("a remote reached over plain HTTP passed without comment")
	}
	// Loopback is the developer's MinIO and is not a warning: the credentials
	// never leave the host.
	c = base()
	c.Backup.Remotes[0].Endpoint = "http://127.0.0.1:9000"
	if hasCode(c.Validate(), "backup.remote_insecure") {
		t.Error("a remote on loopback was warned about as if it crossed a network")
	}
}

// A half-written remote is the dangerous case: it looks configured, it copies
// nothing, and the fleet believes it has an offsite backup it has never had.
func TestAHalfWrittenRemoteIsAnErrorRatherThanSilence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		remote BackupRemote
		code   string
	}{
		{"no bucket", BackupRemote{Name: "offsite", Endpoint: "https://s3.example.com"}, "backup.remote_incomplete"},
		{"no endpoint", BackupRemote{Name: "offsite", Bucket: "acme"}, "backup.remote_incomplete"},
		{
			"no credentials",
			BackupRemote{Name: "offsite", Endpoint: "https://s3.example.com", Bucket: "acme"},
			"backup.remote_credentials",
		},
		{
			"an endpoint that is not a URL",
			BackupRemote{Name: "offsite", Endpoint: "s3.example.com", Bucket: "acme", AccessKeyID: "a", SecretAccessKey: "b"},
			"backup.remote_endpoint",
		},
		{
			"a name that is not a name",
			BackupRemote{Name: "Off Site!", Endpoint: "https://s3.example.com", Bucket: "acme", AccessKeyID: "a", SecretAccessKey: "b"},
			"backup.remote_name",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := Default()
			c.Backup.Remotes = []BackupRemote{tc.remote}
			// Deliberately not normalised for the name case: normalize
			// lower-cases and trims, and the validator is what refuses what is
			// left.
			if !hasCode(c.Validate(), tc.code) {
				t.Errorf("%+v raised no %s", tc.remote, tc.code)
			}
		})
	}

	// Two destinations with one name: one of them would be unaddressable.
	c := Default()
	c.Backup.Remotes = []BackupRemote{
		{Name: "offsite", Endpoint: "https://s3.example.com", Bucket: "a", AccessKeyID: "a", SecretAccessKey: "b"},
		{Name: "offsite", Endpoint: "https://s3.example.com", Bucket: "b", AccessKeyID: "a", SecretAccessKey: "b"},
	}
	if !hasCode(c.Validate(), "backup.remote_duplicate") {
		t.Error("two remotes sharing a name passed without comment")
	}

	// A remote switched off is not a remote to complain about: that is what
	// disabled is for.
	c = Default()
	c.Backup.Remotes = []BackupRemote{{Name: "offsite", Disabled: true}}
	c.normalize()
	for _, f := range c.Validate() {
		if strings.HasPrefix(f.Code, "backup.remote_") {
			t.Errorf("a disabled remote raised %s", f.Code)
		}
	}
}

// Nothing in a remote reaches a manifest, a settings export or a diagnostics
// bundle, and that is a property of the registry rather than of a list of
// fields somebody has to remember to blank.
func TestARemotesCredentialsAreNotInTheRedactedConfiguration(t *testing.T) {
	c := Default()
	c.Backup.Remotes = []BackupRemote{{
		Name: "offsite", Endpoint: "https://s3.example.com", Bucket: "acme",
		AccessKeyID: "AKIAEXAMPLE", SecretAccessKey: "the secret", Passphrase: "the passphrase",
	}}
	blob := Redacted(c)
	if backupSection, ok := blob["backup"].(map[string]any); ok {
		if _, leaked := backupSection["remotes"]; leaked {
			t.Fatal("the backup remotes are in the redacted configuration, which is what a manifest carries")
		}
	}
}
