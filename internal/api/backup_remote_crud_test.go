package api

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/backup"
	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/controller"
)

// Adding a destination from the page, which is the half a file could not do:
// typed, tested, stored with its secret sealed, and used by the next pass.
func TestADestinationAddedOnThePageIsTestedStoredAndUsed(t *testing.T) {
	fake := backup.NewFakeS3("backups")
	t.Cleanup(fake.Close)
	h := newHarness(t, allowPrivateEgress)
	h.installation()
	cookie := h.platform()

	// Distinctive values, so "is the secret in the response?" is a question
	// with an exact answer rather than a search for the word "secret" among
	// field names that also contain it.
	const (
		secretKey  = "s3cr3t-value-never-served"
		passphrase = "passphrase-value-never-served"
	)
	body := map[string]any{
		"name": "offsite", "endpoint": fake.Endpoint(), "bucket": fake.Bucket(),
		"prefix": "fleet", "access_key_id": "AKIAEXAMPLE", "secret_access_key": secretKey,
		"passphrase": passphrase, "keep": 30,
	}

	// Tested before it is saved, which is the whole reason the button is there.
	resp := h.do(request{method: http.MethodPost, path: "/api/v1/backups/remotes/check", cookie: cookie, body: body})
	resp.mustStatus(t, http.StatusOK, "test a destination that is not saved yet")
	var check remoteCheckResponse
	resp.into(t, &check)
	if !check.OK {
		t.Fatalf("the draft was refused: %+v", check)
	}

	resp = h.do(request{method: http.MethodPost, path: "/api/v1/backups/remotes", cookie: cookie, body: body})
	resp.mustStatus(t, http.StatusCreated, "add a destination")
	var added controller.RemoteBackupStatus
	resp.into(t, &added)
	if added.Source != controller.RemoteSourceDatabase || added.ID == "" {
		t.Fatalf("what came back is %+v; a destination added here is a stored one", added)
	}
	if !added.Encrypted || !added.HasSecretKey || added.Keep != 30 {
		t.Errorf("the destination reads %+v", added)
	}

	// Neither secret ever comes back, in any shape. They are sealed at rest
	// and one-way on the wire: what a page can act on is whether one is set.
	if strings.Contains(string(resp.body), secretKey) || strings.Contains(string(resp.body), passphrase) {
		t.Fatalf("a secret was served back: %s", string(resp.body))
	}
	listing := h.do(request{method: http.MethodGet, path: "/api/v1/backups", cookie: cookie})
	if strings.Contains(string(listing.body), secretKey) || strings.Contains(string(listing.body), passphrase) {
		t.Fatalf("the backups page carries a secret: %s", string(listing.body))
	}

	// And it is a destination the fleet actually uses: taking a backup and
	// running the pass puts a copy in the bucket.
	taken := h.takeBackup(t, cookie)
	resp = h.do(request{method: http.MethodPost, path: "/api/v1/backups/offsite", cookie: cookie})
	resp.mustStatus(t, http.StatusOK, "copy offsite")
	var shipped shipResponse
	resp.into(t, &shipped)
	if shipped.Error != "" || len(shipped.Sent) != 1 || shipped.Sent[0].ID != taken.ID {
		t.Fatalf("the pass sent %+v (%s)", shipped.Sent, shipped.Error)
	}
	if keys := fake.Keys(); len(keys) != 1 || !strings.HasSuffix(keys[0], ".tar.gz"+backup.EncryptedExt) {
		t.Errorf("the bucket holds %v; the destination has a passphrase, so the archive is sealed", keys)
	}
}

// The two secrets follow the credential convention, and the difference between
// "leave it" and "clear it" is the difference between a sealed archive and a
// readable one -- so it is pinned rather than assumed.
func TestChangingADestinationLeavesTheSecretsItDoesNotMention(t *testing.T) {
	fake := backup.NewFakeS3("backups")
	t.Cleanup(fake.Close)
	h := newHarness(t, allowPrivateEgress)
	cookie := h.platform()

	create := map[string]any{
		"name": "offsite", "endpoint": fake.Endpoint(), "bucket": fake.Bucket(),
		"access_key_id": "AKIAEXAMPLE", "secret_access_key": "secret", "passphrase": "a long passphrase",
	}
	h.do(request{method: http.MethodPost, path: "/api/v1/backups/remotes", cookie: cookie, body: create}).
		mustStatus(t, http.StatusCreated, "add a destination")

	// A PATCH about the retention says nothing about the secrets, so both stay.
	resp := h.do(request{method: http.MethodPatch, path: "/api/v1/backups/remotes/offsite", cookie: cookie,
		body: map[string]any{"keep": 3}})
	resp.mustStatus(t, http.StatusOK, "change the retention")
	var patched controller.RemoteBackupStatus
	resp.into(t, &patched)
	if !patched.Encrypted || !patched.HasSecretKey || patched.Keep != 3 {
		t.Fatalf("the destination reads %+v after a change about the retention alone", patched)
	}

	// An explicit empty passphrase clears it, which is how a destination is
	// told to send the plain archive.
	resp = h.do(request{method: http.MethodPatch, path: "/api/v1/backups/remotes/offsite", cookie: cookie,
		body: map[string]any{"passphrase": ""}})
	resp.mustStatus(t, http.StatusOK, "clear the passphrase")
	resp.into(t, &patched)
	if patched.Encrypted {
		t.Error("an explicit empty passphrase did not clear the stored one")
	}
	if !patched.HasSecretKey {
		t.Error("clearing the passphrase also cleared the secret key")
	}
}

// A destination the file describes is not editable here, and a stored one may
// not take a name the file has: the file has the last word, and a page that
// let somebody write a row which would then be ignored would be lying.
func TestTheConfigurationFileHasTheLastWordOnADestinationsName(t *testing.T) {
	fake := backup.NewFakeS3("backups")
	t.Cleanup(fake.Close)
	h := newHarness(t, func(c *config.Config) {
		c.Backup.Remotes = []config.BackupRemote{{
			Name: "offsite", Endpoint: fake.Endpoint(), Bucket: fake.Bucket(),
			AccessKeyID: "AKIAEXAMPLE", SecretAccessKey: "secret",
		}}
		c.Normalize()
	})
	cookie := h.platform()

	resp := h.do(request{method: http.MethodPost, path: "/api/v1/backups/remotes", cookie: cookie,
		body: map[string]any{
			"name": "offsite", "endpoint": fake.Endpoint(), "bucket": fake.Bucket(),
			"access_key_id": "a", "secret_access_key": "b",
		}})
	resp.mustStatus(t, http.StatusUnprocessableEntity, "add a destination the file already names")
	if !strings.Contains(string(resp.body), "zoomies.yaml") {
		t.Errorf("the refusal reads %s, which does not say where the other one is", string(resp.body))
	}

	// And editing the file's destination is refused with the same explanation
	// rather than a 404, which would read as "it is not there".
	resp = h.do(request{method: http.MethodPatch, path: "/api/v1/backups/remotes/offsite", cookie: cookie,
		body: map[string]any{"keep": 3}})
	resp.mustStatus(t, http.StatusConflict, "edit a destination the file describes")

	// The page says which is which.
	resp = h.do(request{method: http.MethodGet, path: "/api/v1/backups", cookie: cookie})
	resp.mustStatus(t, http.StatusOK, "list backups")
	var listed backupsResponse
	resp.into(t, &listed)
	if len(listed.Remotes) != 1 || listed.Remotes[0].Source != controller.RemoteSourceFile {
		t.Fatalf("the page shows %+v", listed.Remotes)
	}
}

// A destination that cannot work is refused with the reason under the field
// that is wrong, because the alternative is a form that saves and a bucket
// that never receives anything.
func TestADestinationThatCouldNotWorkIsRefusedByField(t *testing.T) {
	h := newHarness(t)
	cookie := h.platform()

	for _, tc := range []struct {
		name  string
		body  map[string]any
		field string
	}{
		{"no bucket", map[string]any{"name": "offsite", "endpoint": "https://s3.example.com", "secret_access_key": "s"}, "bucket"},
		{"an endpoint that is not a URL", map[string]any{"name": "offsite", "endpoint": "s3.example.com", "bucket": "b", "secret_access_key": "s"}, "endpoint"},
		{"no secret key", map[string]any{"name": "offsite", "endpoint": "https://s3.example.com", "bucket": "b"}, "secret_access_key"},
		{"a name that is not a name", map[string]any{"name": "Off Site!", "endpoint": "https://s3.example.com", "bucket": "b", "secret_access_key": "s"}, "name"},
		{"a name the API already uses", map[string]any{"name": "check", "endpoint": "https://s3.example.com", "bucket": "b", "secret_access_key": "s"}, "name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := h.do(request{method: http.MethodPost, path: "/api/v1/backups/remotes", cookie: cookie, body: tc.body})
			resp.mustStatus(t, http.StatusUnprocessableEntity, tc.name)
			if !strings.Contains(string(resp.body), `"`+tc.field+`"`) {
				t.Errorf("the refusal is %s, which does not name the %s field", string(resp.body), tc.field)
			}
		})
	}
}

// Removing a destination forgets where the copies are; it does not delete
// them. An operator who meant the other thing removes them from the listing,
// where each one is named.
func TestRemovingADestinationLeavesTheCopiesInTheBucket(t *testing.T) {
	fake := backup.NewFakeS3("backups")
	t.Cleanup(fake.Close)
	h := newHarness(t, allowPrivateEgress)
	cookie := h.platform()

	h.do(request{method: http.MethodPost, path: "/api/v1/backups/remotes", cookie: cookie,
		body: map[string]any{
			"name": "offsite", "endpoint": fake.Endpoint(), "bucket": fake.Bucket(),
			"access_key_id": "AKIAEXAMPLE", "secret_access_key": "secret",
		}}).mustStatus(t, http.StatusCreated, "add a destination")

	h.takeBackup(t, cookie)
	h.do(request{method: http.MethodPost, path: "/api/v1/backups/offsite", cookie: cookie}).
		mustStatus(t, http.StatusOK, "copy offsite")
	if fake.Count() != 1 {
		t.Fatalf("the bucket holds %d copies before the destination is removed", fake.Count())
	}

	h.do(request{method: http.MethodDelete, path: "/api/v1/backups/remotes/offsite", cookie: cookie}).
		mustStatus(t, http.StatusNoContent, "remove the destination")
	if fake.Count() != 1 {
		t.Errorf("removing the destination deleted %d copies from the bucket", 1-fake.Count())
	}

	resp := h.do(request{method: http.MethodGet, path: "/api/v1/backups", cookie: cookie})
	resp.mustStatus(t, http.StatusOK, "list backups")
	var listed backupsResponse
	resp.into(t, &listed)
	if len(listed.Remotes) != 0 {
		t.Errorf("the page still shows %+v", listed.Remotes)
	}
}

// "Backups are taken but never leave this host" is raised by the validator,
// which reads the configuration file and cannot see a destination stored in
// the database. Telling somebody to do a thing they have already done is how a
// page teaches them to stop reading it, so the settings API drops that finding
// once the fleet has a destination -- wherever it came from.
func TestTheNoRemoteFindingGoesAwayWhenADestinationIsAdded(t *testing.T) {
	fake := backup.NewFakeS3("backups")
	t.Cleanup(fake.Close)
	h := newHarness(t, allowPrivateEgress)
	cookie := h.platform()

	codes := func() []string {
		resp := h.do(request{method: http.MethodGet, path: "/api/v1/settings", cookie: cookie})
		resp.mustStatus(t, http.StatusOK, "read the settings")
		var out struct {
			Findings []struct {
				Code string `json:"code"`
			} `json:"findings"`
		}
		resp.into(t, &out)
		var found []string
		for _, f := range out.Findings {
			found = append(found, f.Code)
		}
		return found
	}

	if !slices.Contains(codes(), config.NoRemoteFinding) {
		t.Fatalf("a fleet whose backups never leave the host is not told: %v", codes())
	}

	h.do(request{method: http.MethodPost, path: "/api/v1/backups/remotes", cookie: cookie,
		body: map[string]any{
			"name": "offsite", "endpoint": fake.Endpoint(), "bucket": fake.Bucket(),
			"access_key_id": "AKIAEXAMPLE", "secret_access_key": "secret",
		}}).mustStatus(t, http.StatusCreated, "add a destination")

	if slices.Contains(codes(), config.NoRemoteFinding) {
		t.Errorf("the fleet added a destination and is still told its backups never leave the host: %v", codes())
	}

	// Switched off is not answered: a destination nothing is sent to is not a
	// destination.
	h.do(request{method: http.MethodPatch, path: "/api/v1/backups/remotes/offsite", cookie: cookie,
		body: map[string]any{"enabled": false}}).mustStatus(t, http.StatusOK, "switch it off")
	if !slices.Contains(codes(), config.NoRemoteFinding) {
		t.Errorf("a fleet whose only destination is switched off is told nothing: %v", codes())
	}
}

// allowPrivateEgress is what a harness needs to talk to a fake that httptest
// has bound to 127.0.0.1 -- the same switch an operator with a MinIO beside
// the controller sets, rather than a hole in the guard only tests can use.
func allowPrivateEgress(c *config.Config) { c.Security.AllowPrivateEgress = true }

// A destination on this machine or its network is somewhere the controller
// would send the whole fleet's backup and its access key on an
// administrator's say-so, so it is refused by field, naming the switch, and
// admitted once the platform has set it.
func TestADestinationOnAPrivateAddressIsRefusedUntilThePlatformAllowsIt(t *testing.T) {
	for _, tc := range []struct {
		name  string
		allow bool
		want  int
	}{
		{"refused by default", false, http.StatusUnprocessableEntity},
		{"admitted by security.allow_private_egress", true, http.StatusCreated},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, func(c *config.Config) { c.Security.AllowPrivateEgress = tc.allow })
			h.installation()
			cookie := h.platform()
			resp := h.do(request{method: http.MethodPost, path: "/api/v1/backups/remotes", cookie: cookie, body: map[string]any{
				"name": "lan", "endpoint": "http://169.254.169.254", "bucket": "b",
				"access_key_id": "AKIAEXAMPLE", "secret_access_key": "secret",
			}})
			resp.mustStatus(t, tc.want, "create a destination at the metadata address")
			if tc.allow {
				return
			}
			if body := string(resp.body); !strings.Contains(body, `"field":"endpoint"`) || !strings.Contains(body, "security.allow_private_egress") {
				t.Errorf("the refusal does not name the field and the setting that would allow it: %s", body)
			}
		})
	}
}
