package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/backup"
	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/controller"
	"github.com/eyupio/zoomies/internal/store"
)

// The Backups tab's offsite half. What these protect is the sequence an
// operator actually performs on the day the local copies are gone: see which
// destinations exist, prove one can be reached, look at what it holds, and
// bring a copy back -- landing as an ordinary backup, so that the restore is
// still the staged, confirmed one rather than something that happened inside a
// download.

// withRemote gives the harness a fleet whose backups go to a fake bucket.
func withRemote(t *testing.T) (*harness, *backup.FakeS3) {
	t.Helper()
	fake := backup.NewFakeS3("backups")
	t.Cleanup(fake.Close)
	h := newHarness(t, func(c *config.Config) {
		c.Backup.Remotes = []config.BackupRemote{{
			Name: "offsite", Endpoint: fake.Endpoint(), Bucket: fake.Bucket(), Prefix: "fleet",
			AccessKeyID: "AKIAEXAMPLE", SecretAccessKey: "secret",
		}}
		c.Normalize()
	})
	return h, fake
}

func TestABackupIsSentOffsiteListedThereAndBroughtBack(t *testing.T) {
	h, fake := withRemote(t)
	h.installation()
	cookie := h.platform()
	taken := h.takeBackup(t, cookie)

	// The page says where copies go before any has gone.
	resp := h.do(request{method: http.MethodGet, path: "/api/v1/backups", cookie: cookie})
	resp.mustStatus(t, http.StatusOK, "list backups")
	var listed backupsResponse
	resp.into(t, &listed)
	if len(listed.Remotes) != 1 || listed.Remotes[0].Name != "offsite" || listed.Remotes[0].Encrypted {
		t.Fatalf("the page shows %+v as its destinations", listed.Remotes)
	}

	resp = h.do(request{method: http.MethodPost, path: "/api/v1/backups/remotes/offsite/check", cookie: cookie})
	resp.mustStatus(t, http.StatusOK, "check the remote")
	var check remoteCheckResponse
	resp.into(t, &check)
	if !check.OK || check.Where != "backups/fleet" {
		t.Errorf("the check says %+v about a bucket that answers", check)
	}

	resp = h.do(request{method: http.MethodPost, path: "/api/v1/backups/offsite", cookie: cookie})
	resp.mustStatus(t, http.StatusOK, "copy the backups offsite")
	var shipped shipResponse
	resp.into(t, &shipped)
	if shipped.Error != "" || len(shipped.Sent) != 1 || shipped.Sent[0].ID != taken.ID {
		t.Fatalf("the pass sent %+v (%s)", shipped.Sent, shipped.Error)
	}

	resp = h.do(request{method: http.MethodGet, path: "/api/v1/backups/remotes/offsite/copies", cookie: cookie})
	resp.mustStatus(t, http.StatusOK, "list what the remote holds")
	var copies remoteCopiesResponse
	resp.into(t, &copies)
	if len(copies.Items) != 1 || copies.Items[0].ID != taken.ID || copies.Bytes == 0 {
		t.Fatalf("the remote lists %+v", copies)
	}
	if copies.Remote.Name != "offsite" {
		t.Errorf("the listing does not say which remote it came from: %+v", copies.Remote)
	}

	// The local copy goes, as it would if the disk had. What comes back is an
	// ordinary backup: verified, listed, and marked as having come from
	// offsite so that retention leaves it alone.
	if err := backup.Delete(h.ctrl.BackupDir(), taken.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	resp = h.do(request{
		method: http.MethodPost,
		path:   "/api/v1/backups/remotes/offsite/copies/" + taken.ID + "/fetch",
		cookie: cookie,
	})
	resp.mustStatus(t, http.StatusCreated, "bring the copy back")
	var back backupView
	resp.into(t, &back)
	if back.ID != taken.ID || back.Source != backup.SourceFetched || !back.Restorable {
		t.Fatalf("what came back reads %+v", back)
	}

	// And removing the offsite copy is a write, audited like any other.
	resp = h.do(request{
		method: http.MethodDelete,
		path:   "/api/v1/backups/remotes/offsite/copies/" + taken.ID,
		cookie: cookie,
	})
	resp.mustStatus(t, http.StatusNoContent, "remove the offsite copy")
	if fake.Count() != 0 {
		t.Errorf("the bucket still holds %d objects", fake.Count())
	}
}

// A bucket that refuses is not this API failing. The check reports it as a
// result, and the listing as a refusal the operator can act on -- both
// carrying the service's own words, because "500" is not a fix.
func TestARemoteThatRefusesIsReportedRatherThanHidden(t *testing.T) {
	h, fake := withRemote(t)
	cookie := h.platform()
	fake.BreakWith(http.StatusForbidden, "SignatureDoesNotMatch", "no")

	resp := h.do(request{method: http.MethodPost, path: "/api/v1/backups/remotes/offsite/check", cookie: cookie})
	resp.mustStatus(t, http.StatusOK, "check a remote that refuses")
	var check remoteCheckResponse
	resp.into(t, &check)
	if check.OK || check.Error == "" {
		t.Fatalf("the check says %+v about a bucket that refused it", check)
	}

	resp = h.do(request{method: http.MethodGet, path: "/api/v1/backups/remotes/offsite/copies", cookie: cookie})
	resp.mustStatus(t, http.StatusUnprocessableEntity, "list a remote that refuses")
}

// A name that is not a remote is a 404 rather than a 500, and a fleet with no
// remotes configured says so in its listing rather than pretending.
func TestAnUnknownRemoteIsNotFoundAndNoRemotesIsAnEmptyList(t *testing.T) {
	h, _ := withRemote(t)
	cookie := h.platform()
	for _, path := range []string{
		"/api/v1/backups/remotes/nowhere/copies",
		"/api/v1/backups/remotes/nowhere/check",
	} {
		method := http.MethodGet
		if path == "/api/v1/backups/remotes/nowhere/check" {
			method = http.MethodPost
		}
		resp := h.do(request{method: method, path: path, cookie: cookie})
		resp.mustStatus(t, http.StatusNotFound, "a remote that is not configured")
	}

	plain := newHarness(t)
	resp := plain.do(request{method: http.MethodGet, path: "/api/v1/backups", cookie: plain.platform()})
	resp.mustStatus(t, http.StatusOK, "list backups with no remotes")
	var listed backupsResponse
	resp.into(t, &listed)
	if len(listed.Remotes) != 0 {
		t.Errorf("a fleet with no remotes reports %+v", listed.Remotes)
	}
}

// Retention on demand, which exists because lowering the number of copies a
// fleet keeps otherwise does nothing at all until the next backup -- and on a
// fleet with the schedule off, nothing ever. The route has to reach both
// halves, and say what it removed from each: the local ceiling and the
// bucket's are separate numbers and a single total would read as a fault.
func TestRetentionIsAppliedOnDemandToBothHalves(t *testing.T) {
	h, fake := withRemote(t)
	h.installation()
	cookie := h.platform()

	h.ctrl.UpdateConfig(func(c *config.Config) { c.Backup.Keep = 0 })
	var taken []string
	for range 3 {
		taken = append(taken, h.takeBackup(t, cookie).ID)
		// The directory name carries the clock's second.
		time.Sleep(1100 * time.Millisecond)
	}
	resp := h.do(request{method: http.MethodPost, path: "/api/v1/backups/offsite", cookie: cookie})
	resp.mustStatus(t, http.StatusOK, "copy the backups offsite")
	if got := fake.Count(); got != 3 {
		t.Fatalf("setting up: the bucket holds %d copies", got)
	}

	h.ctrl.UpdateConfig(func(c *config.Config) {
		c.Backup.Keep = 1
		c.Backup.Remotes[0].Keep = 2
	})
	resp = h.do(request{method: http.MethodPost, path: "/api/v1/backups/prune", cookie: cookie})
	resp.mustStatus(t, http.StatusOK, "apply retention")
	var report controller.BackupPruning
	resp.into(t, &report)

	if report.Keep != 1 || len(report.Removed) != 2 || report.Error != "" {
		t.Errorf("the local half of the report reads %+v", report)
	}
	if len(report.Remotes) != 1 || report.Remotes[0].Keep != 2 || len(report.Remotes[0].Removed) != 1 {
		t.Fatalf("the offsite half of the report reads %+v", report.Remotes)
	}
	if got := fake.Count(); got != 2 {
		t.Errorf("the bucket holds %d copies with keep: 2", got)
	}

	// And the page agrees: one backup left here, the newest.
	resp = h.do(request{method: http.MethodGet, path: "/api/v1/backups", cookie: cookie})
	resp.mustStatus(t, http.StatusOK, "list backups")
	var listed backupsResponse
	resp.into(t, &listed)
	if len(listed.Items) != 1 || listed.Items[0].ID != taken[len(taken)-1] {
		t.Errorf("the page lists %d backups after retention was applied", len(listed.Items))
	}
}

// Pruning deletes copies of the whole fleet, so it is a write rather than
// something a read-only token can set off.
func TestRetentionOnDemandNeedsTheAdministratorRole(t *testing.T) {
	h, _ := withRemote(t)
	h.installation()
	u, _ := h.user("viewer", store.RoleViewer)
	resp := h.do(request{method: http.MethodPost, path: "/api/v1/backups/prune", cookie: h.session(u)})
	resp.mustStatus(t, http.StatusForbidden, "prune as a viewer")
}
