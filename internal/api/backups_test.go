package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/backup"
	"github.com/eyupio/zoomies/internal/store"
)

// The Backups tab is the one place in the product where a copy of the whole
// database leaves the machine and where one comes back to replace the fleet,
// so what these tests protect is the sequence: a backup taken from the page is
// the same backup the command line restores, a download round-trips through
// an upload, and a restore is staged with every check made now and nothing
// moved until the restart.

// admin is a signed-in administrator's cookie.
// platform is a session for the role the backup routes now need. A backup is
// the whole database under the key this host holds, so it belongs to whoever
// runs the process rather than to the fleet's administrator.
func (h *harness) platform() string {
	h.t.Helper()
	u, _ := h.user("platform", store.RolePlatform)
	return h.session(u)
}

// takeBackup presses the button and returns what the page would show.
func (h *harness) takeBackup(t *testing.T, cookie string) backupView {
	t.Helper()
	resp := h.do(request{method: http.MethodPost, path: "/api/v1/backups", cookie: cookie})
	resp.mustStatus(t, http.StatusCreated, "take a backup")
	var v backupView
	resp.into(t, &v)
	return v
}

func TestABackupTakenFromThePageIsListedWithWhatARestoreNeedsToKnow(t *testing.T) {
	h := newHarness(t)
	h.installation()
	cookie := h.platform()

	taken := h.takeBackup(t, cookie)
	if taken.Source != backup.SourceManual || taken.TakenBy != "platform" || taken.Bytes == 0 {
		t.Errorf("taken = %+v", taken)
	}
	if taken.KeyMatches == nil || !*taken.KeyMatches || taken.Secrets != 1 || !taken.Restorable {
		t.Errorf("the row does not say the key matches and the backup is restorable: %+v", taken)
	}

	resp := h.do(request{method: http.MethodGet, path: "/api/v1/backups", cookie: cookie})
	resp.mustStatus(t, http.StatusOK, "list backups")
	var listed backupsResponse
	resp.into(t, &listed)
	if len(listed.Items) != 1 || listed.Items[0].ID != taken.ID {
		t.Fatalf("listed = %+v", listed.Items)
	}
	if listed.Directory != h.ctrl.BackupDir() || listed.DatabaseBytes == 0 && listed.Directory == "" {
		t.Errorf("the page does not know where backups go: %+v", listed)
	}
	if !listed.Schedule.Enabled || listed.Schedule.Interval != "24h" || listed.Schedule.NextDueAt == nil {
		t.Errorf("schedule = %+v", listed.Schedule)
	}
	if listed.StagedRestore != nil || listed.LastRestore != nil || listed.Restarting {
		t.Errorf("a fresh instance claims a restore is in progress: %+v", listed)
	}

	// The manifest on disk is the one the command line reads.
	m, err := backup.ReadManifest(filepath.Join(listed.Directory, taken.ID))
	if err != nil || m.Source != backup.SourceManual {
		t.Errorf("the backup on disk is not what the page reported: %+v, %v", m, err)
	}

	// And the whole page's shape is what the contract promises.
	doc := loadSpec(t)
	assertShape(t, doc, "Backups", resp.body)
	one := h.do(request{method: http.MethodGet, path: "/api/v1/backups/" + taken.ID, cookie: cookie})
	one.mustStatus(t, http.StatusOK, "get backup")
	assertShape(t, doc, "Backup", one.body)
}

// A download is the backup leaving the machine and an upload is it coming
// back, so the two have to agree exactly: plain, and encrypted with a
// passphrase that is then required.
func TestADownloadRoundTripsThroughAnUploadPlainAndEncrypted(t *testing.T) {
	h := newHarness(t)
	h.installation()
	cookie := h.platform()
	taken := h.takeBackup(t, cookie)

	plain := h.do(request{method: http.MethodGet, path: "/api/v1/backups/" + taken.ID + "/download", cookie: cookie})
	plain.mustStatus(t, http.StatusOK, "download")
	if cd := plain.header.Get("Content-Disposition"); !strings.Contains(cd, taken.ID+".tar.gz") {
		t.Errorf("Content-Disposition = %q", cd)
	}
	if !bytes.HasPrefix(plain.body, []byte("\x1f\x8b")) {
		t.Fatal("the download is not a gzipped archive")
	}

	enc := h.do(request{method: http.MethodPost, path: "/api/v1/backups/" + taken.ID + "/download", cookie: cookie,
		body: map[string]any{"passphrase": "correct horse battery staple"}})
	enc.mustStatus(t, http.StatusOK, "encrypted download")
	if cd := enc.header.Get("Content-Disposition"); !strings.Contains(cd, ".tar.gz.enc") {
		t.Errorf("Content-Disposition = %q", cd)
	}
	if bytes.HasPrefix(enc.body, []byte("\x1f\x8b")) || bytes.Contains(enc.body, []byte("manifest.json")) {
		t.Fatal("the encrypted download is readable")
	}
	short := h.do(request{method: http.MethodPost, path: "/api/v1/backups/" + taken.ID + "/download", cookie: cookie,
		body: map[string]any{"passphrase": "1234"}})
	short.mustStatus(t, http.StatusUnprocessableEntity, "a four-character passphrase")

	// The original gone, both come back under the original's name.
	del := h.do(request{method: http.MethodDelete, path: "/api/v1/backups/" + taken.ID, cookie: cookie})
	del.mustStatus(t, http.StatusNoContent, "delete")

	up := h.upload(t, cookie, "backup.tar.gz", plain.body, "")
	up.mustStatus(t, http.StatusCreated, "plain upload")
	var uploaded backupView
	up.into(t, &uploaded)
	if uploaded.ID != taken.ID || uploaded.Source != backup.SourceUploaded || uploaded.TakenBy != "platform" {
		t.Errorf("uploaded = %+v", uploaded)
	}

	wrong := h.upload(t, cookie, "backup.tar.gz.enc", enc.body, "not the passphrase")
	wrong.mustStatus(t, http.StatusUnprocessableEntity, "encrypted upload with the wrong passphrase")
	if msg := string(wrong.body); !strings.Contains(msg, "passphrase") {
		t.Errorf("the refusal does not name the passphrase: %s", msg)
	}
	missing := h.upload(t, cookie, "backup.tar.gz.enc", enc.body, "")
	missing.mustStatus(t, http.StatusUnprocessableEntity, "encrypted upload with no passphrase")

	right := h.upload(t, cookie, "backup.tar.gz.enc", enc.body, "correct horse battery staple")
	right.mustStatus(t, http.StatusCreated, "encrypted upload")
	var second backupView
	right.into(t, &second)
	if second.ID == uploaded.ID || !strings.HasPrefix(second.ID, taken.ID) {
		t.Errorf("a second upload of the same backup landed as %s", second.ID)
	}

	// The uploads are audited as what they are, and so was the download.
	audit := h.do(request{method: http.MethodGet, path: "/api/v1/audit?action=backup.download&action=backup.upload", cookie: cookie})
	audit.mustStatus(t, http.StatusOK, "audit")
	var rows struct {
		Total int `json:"total"`
	}
	audit.into(t, &rows)
	if rows.Total != 4 {
		t.Errorf("audit rows = %d, want two downloads and two uploads", rows.Total)
	}
}

// Garbage in is refused as garbage, and nothing lands in the directory.
func TestAnUploadThatIsNotABackupIsRefused(t *testing.T) {
	h := newHarness(t)
	cookie := h.platform()
	resp := h.upload(t, cookie, "notes.txt", []byte("not an archive"), "")
	resp.mustStatus(t, http.StatusUnprocessableEntity, "upload of a text file")
	if !strings.Contains(string(resp.body), "gzipped") {
		t.Errorf("the refusal does not say what was wrong: %s", resp.body)
	}
	if entries, _ := os.ReadDir(h.ctrl.BackupDir()); len(entries) != 0 {
		t.Errorf("something was left behind: %v", entries)
	}
	raw := h.do(request{method: http.MethodPost, path: "/api/v1/backups/upload", cookie: cookie,
		rawBody: "raw bytes", headers: map[string]string{"Content-Type": "application/octet-stream"}})
	raw.mustStatus(t, http.StatusBadRequest, "a non-multipart upload")
}

// upload posts a multipart form the way the page does.
func (h *harness) upload(t *testing.T, cookie, filename string, archive []byte, passphrase string) *response {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if passphrase != "" {
		if err := mw.WriteField("passphrase", passphrase); err != nil {
			t.Fatal(err)
		}
	}
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(archive); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return h.do(request{method: http.MethodPost, path: "/api/v1/backups/upload", cookie: cookie,
		rawBody: body.String(), headers: map[string]string{"Content-Type": mw.FormDataContentType()}})
}

// Verify re-reads the file. A backup somebody has altered has to come back as
// not OK with a sentence, not as a 500.
func TestVerifyNoticesAnAlteredBackup(t *testing.T) {
	h := newHarness(t)
	cookie := h.platform()
	taken := h.takeBackup(t, cookie)

	ok := h.do(request{method: http.MethodPost, path: "/api/v1/backups/" + taken.ID + "/verify", cookie: cookie})
	ok.mustStatus(t, http.StatusOK, "verify")
	var v backup.Verification
	ok.into(t, &v)
	if !v.OK || !v.DigestMatches {
		t.Errorf("a fresh backup does not verify: %+v", v)
	}
	assertShape(t, loadSpec(t), "BackupVerification", ok.body)

	db := filepath.Join(h.ctrl.BackupDir(), taken.ID, backup.DBName)
	f, err := os.OpenFile(db, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.Write([]byte("altered"))
	f.Close()
	bad := h.do(request{method: http.MethodPost, path: "/api/v1/backups/" + taken.ID + "/verify", cookie: cookie})
	bad.mustStatus(t, http.StatusOK, "verify an altered backup")
	bad.into(t, &v)
	if v.OK || v.DigestMatches || len(v.Problems) == 0 {
		t.Errorf("an altered backup verified: %+v", v)
	}
}

// Staging is the whole of what the page can do about a restore, and it is
// checked now: the wrong key is refused with the sentence, a good backup is
// written down for the next start, the page says so, and cancelling forgets it.
func TestARestoreIsStagedWithEveryCheckMadeNowAndNothingMoved(t *testing.T) {
	h := newHarness(t)
	h.installation()
	cookie := h.platform()
	taken := h.takeBackup(t, cookie)

	// A backup that says it was sealed with another key.
	other := h.takeBackup(t, cookie)
	manifestPath := filepath.Join(h.ctrl.BackupDir(), other.ID, backup.ManifestName)
	raw, _ := os.ReadFile(manifestPath)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	m["key"] = map[string]any{"fingerprint": "deadbeefcafe", "from": "elsewhere"}
	raw, _ = json.Marshal(m)
	_ = os.WriteFile(manifestPath, raw, 0o600)

	listed := h.do(request{method: http.MethodGet, path: "/api/v1/backups", cookie: cookie})
	var page backupsResponse
	listed.into(t, &page)
	for _, item := range page.Items {
		if item.ID == other.ID && (item.Restorable || !strings.Contains(item.RestoreProblem, "deadbeefcafe")) {
			t.Errorf("the list does not warn about the wrong key: %+v", item)
		}
	}
	refused := h.do(request{method: http.MethodPost, path: "/api/v1/backups/" + other.ID + "/restore", cookie: cookie})
	refused.mustStatus(t, http.StatusUnprocessableEntity, "restore with the wrong key")
	if !strings.Contains(refused.errorMessage(t)+string(refused.body), "not the one that sealed this backup") {
		t.Errorf("the refusal does not say why: %s", refused.body)
	}
	// Nothing to apply yet, and a restart with nothing to apply is refused.
	nothing := h.do(request{method: http.MethodPost, path: "/api/v1/backups/restore/apply", cookie: cookie})
	nothing.mustStatus(t, http.StatusConflict, "apply with nothing staged")

	staged := h.do(request{method: http.MethodPost, path: "/api/v1/backups/" + taken.ID + "/restore", cookie: cookie,
		body: map[string]any{"revoke_api_tokens": true}})
	staged.mustStatus(t, http.StatusAccepted, "stage a restore")
	assertShape(t, loadSpec(t), "StagedRestore", staged.body)
	var s backup.Staged
	staged.into(t, &s)
	if s.BackupID != taken.ID || s.RequestedBy != "platform" || !s.RevokeAPITokens || s.ResetAgentTokens {
		t.Errorf("staged = %+v", s)
	}
	// The live database is untouched: the fleet is still running on it.
	if f := h.ctrl.Fenced(); f.Fenced {
		t.Error("staging fenced the running fleet")
	}
	// The staged backup cannot be deleted from under the restart.
	held := h.do(request{method: http.MethodDelete, path: "/api/v1/backups/" + taken.ID, cookie: cookie})
	held.mustStatus(t, http.StatusConflict, "delete a staged backup")
	// A second backup cannot be staged over it.
	over := h.do(request{method: http.MethodPost, path: "/api/v1/backups/" + other.ID + "/restore", cookie: cookie})
	over.mustStatus(t, http.StatusConflict, "stage a second restore")

	listed = h.do(request{method: http.MethodGet, path: "/api/v1/backups", cookie: cookie})
	listed.into(t, &page)
	if page.StagedRestore == nil || page.StagedRestore.BackupID != taken.ID {
		t.Errorf("the page does not show the staged restore: %+v", page.StagedRestore)
	}
	problems := h.do(request{method: http.MethodGet, path: "/api/v1/problems", cookie: cookie})
	if !strings.Contains(string(problems.body), "backup.restore_staged") {
		t.Error("the staged restore is not in the problems drawer")
	}

	cancel := h.do(request{method: http.MethodDelete, path: "/api/v1/backups/restore", cookie: cookie})
	cancel.mustStatus(t, http.StatusNoContent, "cancel")
	if staged, _ := backup.LoadStaged(h.ctrl.DatabasePath()); staged != nil {
		t.Error("the staged restore survived being cancelled")
	}
	again := h.do(request{method: http.MethodDelete, path: "/api/v1/backups/restore", cookie: cookie})
	again.mustStatus(t, http.StatusNoContent, "cancel when nothing is staged")
}

// Applying is the one act that stops the process. It answers before it does,
// and the command that runs the controller sees the request.
func TestApplyingAStagedRestoreAsksForARestart(t *testing.T) {
	h := newHarness(t)
	cookie := h.platform()
	taken := h.takeBackup(t, cookie)
	h.do(request{method: http.MethodPost, path: "/api/v1/backups/" + taken.ID + "/restore", cookie: cookie}).
		mustStatus(t, http.StatusAccepted, "stage")

	resp := h.do(request{method: http.MethodPost, path: "/api/v1/backups/restore/apply", cookie: cookie})
	resp.mustStatus(t, http.StatusAccepted, "apply")
	assertShape(t, loadSpec(t), "Restarting", resp.body)
	select {
	case <-h.ctrl.RestartRequested():
	default:
		t.Fatal("the controller was not asked to restart")
	}
	listed := h.do(request{method: http.MethodGet, path: "/api/v1/backups", cookie: cookie})
	var page backupsResponse
	listed.into(t, &page)
	if !page.Restarting {
		t.Error("the page does not say the controller is restarting")
	}

	// The next start applies it, and the outcome is what the page shows.
	outcome, err := backup.ApplyStaged(context.Background(), h.cfg, nil)
	if err != nil || outcome == nil || !outcome.OK {
		t.Fatalf("ApplyStaged = %+v, %v", outcome, err)
	}
	listed = h.do(request{method: http.MethodGet, path: "/api/v1/backups", cookie: cookie})
	listed.into(t, &page)
	if page.LastRestore == nil || !page.LastRestore.OK || page.LastRestore.BackupID != taken.ID {
		t.Errorf("last restore = %+v", page.LastRestore)
	}
	dismiss := h.do(request{method: http.MethodDelete, path: "/api/v1/backups/restore/outcome", cookie: cookie})
	dismiss.mustStatus(t, http.StatusNoContent, "dismiss")
	listed = h.do(request{method: http.MethodGet, path: "/api/v1/backups", cookie: cookie})
	listed.into(t, &page)
	if page.LastRestore != nil {
		t.Error("the outcome survived being dismissed")
	}
}

// The store's pre-migration copies are backups too, and they have no manifest:
// they are listed as what they are and can be downloaded, with the caveat
// that nothing about them can be checked before restoring.
func TestPreMigrationCopiesAreListedBesideTheBackups(t *testing.T) {
	h := newHarness(t)
	cookie := h.platform()
	dir := filepath.Join(backup.PreMigrationDir(h.ctrl.DatabasePath()), backup.DirPrefix+"20260101-000000")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := h.st.Backup(h.ctx, filepath.Join(dir, backup.DBName)); err != nil {
		t.Fatal(err)
	}
	listed := h.do(request{method: http.MethodGet, path: "/api/v1/backups", cookie: cookie})
	var page backupsResponse
	listed.into(t, &page)
	if len(page.Items) != 1 || page.Items[0].Location != locationPreMigration || page.Items[0].Source != backup.SourcePreMigration {
		t.Fatalf("items = %+v", page.Items)
	}
	if !page.Items[0].Restorable || page.Items[0].RestoreProblem == "" {
		t.Errorf("a copy with no manifest should be restorable with a caveat: %+v", page.Items[0])
	}
	dl := h.do(request{method: http.MethodGet, path: "/api/v1/backups/" + page.Items[0].ID + "/download", cookie: cookie})
	dl.mustStatus(t, http.StatusOK, "download a pre-migration copy")
	if _, err := io.ReadAll(bytes.NewReader(dl.body)); err != nil {
		t.Fatal(err)
	}
}

// The id is a path component. One that is not a backup id is refused before
// it touches the filesystem, and one that names nothing is a 404.
func TestBackupIDsThatAreNotBackupIDsAreRefused(t *testing.T) {
	h := newHarness(t)
	cookie := h.platform()
	for _, bad := range []string{"..", "zoomies.db", "zoomies-1"} {
		resp := h.do(request{method: http.MethodGet, path: "/api/v1/backups/" + bad, cookie: cookie})
		if resp.status != http.StatusBadRequest && resp.status != http.StatusNotFound {
			t.Errorf("GET /backups/%s = %d", bad, resp.status)
		}
	}
	gone := h.do(request{method: http.MethodGet, path: "/api/v1/backups/zoomies-19990101-000000", cookie: cookie})
	gone.mustStatus(t, http.StatusNotFound, "a backup that is not there")
}
