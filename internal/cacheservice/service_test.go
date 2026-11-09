package cacheservice

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// token builds a runtime token the way a job's looks to this service: three
// dot-separated parts of which only the middle one is read. perms maps a ref to
// the permission bits the "ac" claim gives it.
func token(perms map[string]int) string {
	var acs []accessClaim
	for ref, p := range perms {
		acs = append(acs, accessClaim{Scope: ref, Permission: p})
	}
	ac, _ := json.Marshal(acs)
	// GitHub writes the claim as a JSON string holding the array.
	payload, _ := json.Marshal(map[string]string{"ac": string(ac)})
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"none"}`)) + "." + enc(payload) + ".sig"
}

var (
	mainToken = token(map[string]int{"refs/heads/main": 3})
	prToken   = token(map[string]int{"refs/pull/7/merge": 3, "refs/heads/main": 1})
)

type harness struct {
	t   *testing.T
	svc *Service
	srv *httptest.Server
	dir string
}

func newHarness(t *testing.T, opts Options) *harness {
	t.Helper()
	if opts.Dir == "" {
		opts.Dir = t.TempDir()
	}
	svc, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(svc)
	t.Cleanup(srv.Close)
	return &harness{t: t, svc: svc, srv: srv, dir: opts.Dir}
}

// call makes one Twirp call and returns the status and the decoded body.
func (h *harness) call(tok, method string, req any) (int, map[string]any) {
	h.t.Helper()
	body, _ := json.Marshal(req)
	r, _ := http.NewRequest(http.MethodPost, h.srv.URL+twirpPrefix+method, bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if tok != "" {
		r.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func (h *harness) put(url string, body []byte) int {
	h.t.Helper()
	r, _ := http.NewRequest(http.MethodPut, url, bytes.NewReader(body))
	r.Header.Set("x-ms-blob-type", "BlockBlob")
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		h.t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// save does a whole small save: reserve, upload, finalise. It returns whether
// the entry now exists.
func (h *harness) save(tok, key, version string, data []byte) bool {
	h.t.Helper()
	status, out := h.call(tok, "CreateCacheEntry", map[string]string{"key": key, "version": version})
	if status != http.StatusOK {
		return false
	}
	if st := h.put(out["signed_upload_url"].(string), data); st != http.StatusCreated {
		h.t.Fatalf("upload answered %d", st)
	}
	_, fin := h.call(tok, "FinalizeCacheEntryUpload", map[string]string{
		"key": key, "version": version, "sizeBytes": fmt.Sprint(len(data))})
	return fin["ok"] == true
}

// restore returns the matched key and the bytes, or "" when nothing matched.
func (h *harness) restore(tok, key, version string, restoreKeys ...string) (string, []byte) {
	h.t.Helper()
	_, out := h.call(tok, "GetCacheEntryDownloadURL", map[string]any{
		"key": key, "restoreKeys": restoreKeys, "version": version})
	if out["ok"] != true {
		return "", nil
	}
	resp, err := http.Get(out["signed_download_url"].(string))
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return out["matched_key"].(string), b
}

// ageEntries makes every finished entry look last used at when, which is how
// the eviction order is steered without sleeping.
func (h *harness) ageEntries(when time.Time) {
	h.t.Helper()
	h.svc.mu.Lock()
	defer h.svc.mu.Unlock()
	for _, e := range h.svc.entries {
		_ = os.Chtimes(h.svc.blobPath(e.ID), when, when)
	}
}

func TestASavedEntryIsRestoredByItsKey(t *testing.T) {
	h := newHarness(t, Options{})
	if !h.save(mainToken, "go-abc", "v1", []byte("the build cache")) {
		t.Fatal("the save did not finish")
	}
	key, got := h.restore(mainToken, "go-abc", "v1")
	if key != "go-abc" || string(got) != "the build cache" {
		t.Fatalf("restored %q with %q", key, got)
	}
}

func TestAnEntryOfAnotherVersionIsAMiss(t *testing.T) {
	// The version hashes the paths and compression, so a key saved for other
	// paths must never be unpacked over these ones.
	h := newHarness(t, Options{})
	h.save(mainToken, "go-abc", "v1", []byte("x"))
	if key, _ := h.restore(mainToken, "go-abc", "v2"); key != "" {
		t.Fatalf("matched %q across versions", key)
	}
}

func TestARestoreKeyMatchesByPrefixAndTheNewestEntryWins(t *testing.T) {
	clock := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	h := newHarness(t, Options{Now: func() time.Time { clock = clock.Add(time.Minute); return clock }})
	h.save(mainToken, "go-linux-1", "v", []byte("old"))
	h.save(mainToken, "go-linux-2", "v", []byte("new"))
	h.save(mainToken, "npm-linux-1", "v", []byte("other"))

	key, got := h.restore(mainToken, "go-linux-9", "v", "go-linux-")
	if key != "go-linux-2" || string(got) != "new" {
		t.Fatalf("restored %q with %q, want the newest go-linux-", key, got)
	}
}

func TestAnExactKeyBeatsARestoreKeyThatIsNewer(t *testing.T) {
	clock := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	h := newHarness(t, Options{Now: func() time.Time { clock = clock.Add(time.Minute); return clock }})
	h.save(mainToken, "go-1", "v", []byte("exact"))
	h.save(mainToken, "go-2", "v", []byte("newer"))
	if key, _ := h.restore(mainToken, "go-1", "v", "go-"); key != "go-1" {
		t.Fatalf("restored %q, want the exact key", key)
	}
}

func TestAKeyAlreadyStoredCannotBeReservedAgain(t *testing.T) {
	h := newHarness(t, Options{})
	h.save(mainToken, "k", "v", []byte("first"))
	status, out := h.call(mainToken, "CreateCacheEntry", map[string]string{"key": "k", "version": "v"})
	if status != http.StatusConflict || out["code"] != "already_exists" {
		t.Fatalf("answered %d %v, want a conflict the toolkit reads as 'another job is saving this'", status, out)
	}
	if _, got := h.restore(mainToken, "k", "v"); string(got) != "first" {
		t.Fatalf("the first entry was replaced: %q", got)
	}
}

func TestAKeyBeingSavedCannotBeReservedByAnotherJob(t *testing.T) {
	h := newHarness(t, Options{})
	if s, _ := h.call(mainToken, "CreateCacheEntry", map[string]string{"key": "k", "version": "v"}); s != http.StatusOK {
		t.Fatal("the first reservation failed")
	}
	if s, _ := h.call(mainToken, "CreateCacheEntry", map[string]string{"key": "k", "version": "v"}); s != http.StatusConflict {
		t.Fatalf("a second reservation answered %d", s)
	}
}

func TestALargeArchiveIsStagedInBlocksAndCommittedInOrder(t *testing.T) {
	h := newHarness(t, Options{})
	_, out := h.call(mainToken, "CreateCacheEntry", map[string]string{"key": "big", "version": "v"})
	url := out["signed_upload_url"].(string)
	b64 := base64.StdEncoding.EncodeToString
	ids := []string{b64([]byte("block-0001")), b64([]byte("block-0002")), b64([]byte("block-0003"))}
	// Staged out of order, as concurrent uploads arrive.
	for _, i := range []int{2, 0, 1} {
		if st := h.put(url+"?comp=block&blockid="+ids[i], []byte(fmt.Sprintf("part%d|", i))); st != http.StatusCreated {
			t.Fatalf("stage %d answered %d", i, st)
		}
	}
	list := "<?xml version=\"1.0\" encoding=\"utf-8\"?><BlockList>"
	for _, id := range ids {
		list += "<Latest>" + id + "</Latest>"
	}
	list += "</BlockList>"
	if st := h.put(url+"?comp=blocklist", []byte(list)); st != http.StatusCreated {
		t.Fatalf("commit answered %d", st)
	}
	want := "part0|part1|part2|"
	_, fin := h.call(mainToken, "FinalizeCacheEntryUpload", map[string]string{"key": "big", "version": "v", "sizeBytes": fmt.Sprint(len(want))})
	if fin["ok"] != true {
		t.Fatalf("finalise answered %v", fin)
	}
	if _, got := h.restore(mainToken, "big", "v"); string(got) != want {
		t.Fatalf("assembled %q, want %q", got, want)
	}
}

func TestAFinaliseCallWithTheWrongSizeSavesNothing(t *testing.T) {
	h := newHarness(t, Options{})
	_, out := h.call(mainToken, "CreateCacheEntry", map[string]string{"key": "k", "version": "v"})
	h.put(out["signed_upload_url"].(string), []byte("12345"))
	_, fin := h.call(mainToken, "FinalizeCacheEntryUpload", map[string]string{"key": "k", "version": "v", "sizeBytes": "99"})
	if fin["ok"] == true {
		t.Fatal("an archive of the wrong size was kept")
	}
	if key, _ := h.restore(mainToken, "k", "v"); key != "" {
		t.Fatal("the unfinished entry can be restored")
	}
	// The slot is free again, so the job's next attempt can save.
	if !h.save(mainToken, "k", "v", []byte("12345")) {
		t.Fatal("the key stayed reserved after a failed save")
	}
}

func TestAnUnfinishedUploadIsNotRestorable(t *testing.T) {
	h := newHarness(t, Options{})
	_, out := h.call(mainToken, "CreateCacheEntry", map[string]string{"key": "k", "version": "v"})
	h.put(out["signed_upload_url"].(string), []byte("data"))
	if key, _ := h.restore(mainToken, "k", "v"); key != "" {
		t.Fatal("an entry that was never finalised was restored")
	}
}

func TestARangedReadReturnsOnlyThoseBytes(t *testing.T) {
	h := newHarness(t, Options{})
	h.save(mainToken, "k", "v", []byte("0123456789"))
	_, out := h.call(mainToken, "GetCacheEntryDownloadURL", map[string]any{"key": "k", "version": "v"})
	r, _ := http.NewRequest(http.MethodGet, out["signed_download_url"].(string), nil)
	r.Header.Set("Range", "bytes=2-5")
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusPartialContent || string(got) != "2345" {
		t.Fatalf("answered %d with %q", resp.StatusCode, got)
	}
	head, _ := http.NewRequest(http.MethodHead, out["signed_download_url"].(string), nil)
	hr, err := http.DefaultClient.Do(head)
	if err != nil {
		t.Fatal(err)
	}
	hr.Body.Close()
	if hr.ContentLength != 10 {
		t.Fatalf("HEAD reported %d bytes, the toolkit sizes its parts from this", hr.ContentLength)
	}
}

// BuildKit reads a layer from the middle with Azure's x-ms-range header, which
// the Go client sends as the only range header it knows. A service that serves
// the whole blob from byte 0 instead hands it a layer that fails its digest.
func TestAnAzureRangeHeaderIsHonouredLikeARangeHeader(t *testing.T) {
	h := newHarness(t, Options{})
	h.save(mainToken, "k", "v", []byte("0123456789"))
	_, out := h.call(mainToken, "GetCacheEntryDownloadURL", map[string]any{"key": "k", "version": "v"})
	r, _ := http.NewRequest(http.MethodGet, out["signed_download_url"].(string), nil)
	r.Header.Set("x-ms-range", "bytes=6-")
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusPartialContent || string(got) != "6789" || resp.Header.Get("ETag") == "" {
		t.Fatalf("answered %d with %q and ETag %q", resp.StatusCode, got, resp.Header.Get("ETag"))
	}
}

// The reply carries the protobuf field names, which is the only spelling
// BuildKit's client reads.
func TestResponsesUseTheProtobufFieldNames(t *testing.T) {
	h := newHarness(t, Options{})
	_, out := h.call(mainToken, "CreateCacheEntry", map[string]any{"key": "k", "version": "v"})
	if _, ok := out["signed_upload_url"].(string); !ok || out["signedUploadUrl"] != nil {
		t.Fatalf("create replied %v", out)
	}
}

// BuildKit's Go client sends size_bytes as a JSON number; the toolkit sends the
// same field as a string. A service that took only one of them would work for
// one client and refuse every save from the other.
func TestTheSizeIsAcceptedAsANumberAsWellAsAString(t *testing.T) {
	h := newHarness(t, Options{})
	for name, size := range map[string]any{"number": 5, "string": "5"} {
		_, out := h.call(mainToken, "CreateCacheEntry", map[string]string{"key": name, "version": "v"})
		h.put(out["signed_upload_url"].(string), []byte("12345"))
		status, fin := h.call(mainToken, "FinalizeCacheEntryUpload", map[string]any{"key": name, "version": "v", "size_bytes": size})
		if status != http.StatusOK || fin["ok"] != true {
			t.Fatalf("a size sent as a %s was refused: %d %v", name, status, fin)
		}
	}
}

// Azure's Go SDK reads x-ms-request-id from an upload's reply without checking
// it is there, and BuildKit's cache client dereferences the result. Without the
// header the builder panics on its first save instead of failing the save.
func TestBlobRepliesCarryAnAzureRequestID(t *testing.T) {
	h := newHarness(t, Options{})
	_, out := h.call(mainToken, "CreateCacheEntry", map[string]string{"key": "k", "version": "v"})
	r, _ := http.NewRequest(http.MethodPut, out["signed_upload_url"].(string), bytes.NewReader([]byte("x")))
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.Header.Get("x-ms-request-id") == "" {
		t.Fatal("an upload reply has no x-ms-request-id")
	}
}

func TestAPullRequestReadsTheDefaultBranchButNeverWritesIt(t *testing.T) {
	h := newHarness(t, Options{})
	h.save(mainToken, "go-1", "v", []byte("from main"))

	if key, got := h.restore(prToken, "go-1", "v"); key != "go-1" || string(got) != "from main" {
		t.Fatalf("the pull request could not read main's entry: %q %q", key, got)
	}
	// Saving the same key from the pull request is its own entry, filed under
	// its own ref, so main's is untouched.
	if !h.save(prToken, "go-1", "v", []byte("from the pr")) {
		t.Fatal("the pull request could not save under its own ref")
	}
	if _, got := h.restore(prToken, "go-1", "v"); string(got) != "from the pr" {
		t.Fatalf("the pull request did not prefer its own entry: %q", got)
	}
	if _, got := h.restore(mainToken, "go-1", "v"); string(got) != "from main" {
		t.Fatalf("main's entry was replaced by the pull request's: %q", got)
	}
}

func TestMainCannotRestoreWhatAPullRequestSaved(t *testing.T) {
	// This is the rule that stops a pull request planting an entry the
	// default branch's next build would unpack.
	h := newHarness(t, Options{})
	h.save(prToken, "only-pr", "v", []byte("planted"))
	if key, _ := h.restore(mainToken, "only-pr", "v"); key != "" {
		t.Fatalf("main restored %q from a pull request", key)
	}
}

func TestATokenWithNoUsableClaimSharesOneScope(t *testing.T) {
	// If GitHub changes the token, the cache becomes shared, as the pool cache
	// is, and does not stop working.
	h := newHarness(t, Options{})
	odd := "x." + base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"job"}`)) + ".y"
	h.save(odd, "k", "v", []byte("shared"))
	other := "a." + base64.RawURLEncoding.EncodeToString([]byte(`{"iss":"x"}`)) + ".b"
	if _, got := h.restore(other, "k", "v"); string(got) != "shared" {
		t.Fatalf("restored %q", got)
	}
}

func TestACallWithoutATokenIsRefused(t *testing.T) {
	h := newHarness(t, Options{})
	status, out := h.call("", "GetCacheEntryDownloadURL", map[string]any{"key": "k", "version": "v"})
	if status != http.StatusUnauthorized || out["code"] != "unauthenticated" {
		t.Fatalf("answered %d %v", status, out)
	}
}

func TestACallWithABadBodyIsAnInvalidArgument(t *testing.T) {
	h := newHarness(t, Options{})
	for _, method := range []string{"CreateCacheEntry", "FinalizeCacheEntryUpload", "GetCacheEntryDownloadURL"} {
		status, out := h.call(mainToken, method, map[string]string{"key": ""})
		if status != http.StatusBadRequest || out["code"] != "invalid_argument" {
			t.Errorf("%s answered %d %v", method, status, out)
		}
	}
	if status, _ := h.call(mainToken, "DeleteCacheEntry", map[string]string{"key": "k"}); status != http.StatusNotFound {
		t.Errorf("an unknown method answered %d", status)
	}
}

func TestEntriesSurviveARestartAndHalfDoneUploadsDoNot(t *testing.T) {
	dir := t.TempDir()
	h := newHarness(t, Options{Dir: dir})
	h.save(mainToken, "kept", "v", []byte("whole"))
	h.call(mainToken, "CreateCacheEntry", map[string]string{"key": "half", "version": "v"})

	again := newHarness(t, Options{Dir: dir})
	if _, got := again.restore(mainToken, "kept", "v"); string(got) != "whole" {
		t.Fatalf("the entry was lost across a restart: %q", got)
	}
	if !again.save(mainToken, "half", "v", []byte("now")) {
		t.Fatal("a reservation outlived the restart that dropped its upload")
	}
}

func TestAnEntryWhoseBytesAreGoneIsForgottenAtStart(t *testing.T) {
	dir := t.TempDir()
	h := newHarness(t, Options{Dir: dir})
	h.save(mainToken, "k", "v", []byte("x"))
	blobs, _ := os.ReadDir(h.svc.blobDir())
	for _, b := range blobs {
		_ = os.Remove(h.svc.blobPath(b.Name()))
	}
	again := newHarness(t, Options{Dir: dir})
	if key, _ := again.restore(mainToken, "k", "v"); key != "" {
		t.Fatal("an entry with no bytes was offered")
	}
}

func TestTheLeastRecentlyUsedEntryIsEvictedFirst(t *testing.T) {
	h := newHarness(t, Options{MaxBytes: 25})
	h.save(mainToken, "a", "v", bytes.Repeat([]byte("a"), 10))
	h.save(mainToken, "b", "v", bytes.Repeat([]byte("b"), 10))
	// Using "a" makes "b" the one least recently used.
	old := time.Now().Add(-time.Hour)
	for _, e := range h.svc.entries {
		if e.Key == "b" {
			_ = os.Chtimes(h.svc.blobPath(e.ID), old, old)
		}
	}
	h.restore(mainToken, "a", "v")
	h.save(mainToken, "c", "v", bytes.Repeat([]byte("c"), 10))

	for key, want := range map[string]bool{"a": true, "b": false, "c": true} {
		got, _ := h.restore(mainToken, key, "v")
		if (got != "") != want {
			t.Errorf("entry %q present = %v, want %v", key, got != "", want)
		}
	}
}

func TestAnEntryJustSavedIsNeverEvictedByItsOwnSave(t *testing.T) {
	// The ceiling fits one entry, so the older one must go and the new one stay.
	h := newHarness(t, Options{MaxBytes: 15})
	h.save(mainToken, "old", "v", bytes.Repeat([]byte("o"), 10))
	old := time.Now().Add(-time.Hour)
	for _, e := range h.svc.entries {
		_ = os.Chtimes(h.svc.blobPath(e.ID), old, old)
	}
	if !h.save(mainToken, "new", "v", bytes.Repeat([]byte("n"), 10)) {
		t.Fatal("the save failed")
	}
	if key, _ := h.restore(mainToken, "new", "v"); key != "new" {
		t.Fatal("the entry that was just written was evicted before anyone could use it")
	}
	if key, _ := h.restore(mainToken, "old", "v"); key != "" {
		t.Fatal("the older entry was kept past the ceiling")
	}
}

func TestAnAbandonedReservationIsReclaimed(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	h := newHarness(t, Options{PendingTTL: time.Hour, Now: func() time.Time { return now }})
	h.call(mainToken, "CreateCacheEntry", map[string]string{"key": "k", "version": "v"})
	if s, _ := h.call(mainToken, "CreateCacheEntry", map[string]string{"key": "k", "version": "v"}); s != http.StatusConflict {
		t.Fatal("the key was not held while the reservation was fresh")
	}
	now = now.Add(2 * time.Hour)
	if s, _ := h.call(mainToken, "CreateCacheEntry", map[string]string{"key": "k", "version": "v"}); s != http.StatusOK {
		t.Fatalf("a job that died holding a reservation blocks the key for ever (%d)", s)
	}
}

func TestOnlyAReservedUploadCanBeWritten(t *testing.T) {
	h := newHarness(t, Options{})
	for _, path := range []string{"/_blob/nope/archive", "/_blob/../etc/archive", "/_blob/", "/elsewhere"} {
		r, _ := http.NewRequest(http.MethodPut, h.srv.URL+path, strings.NewReader("x"))
		resp, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("PUT %s answered %d", path, resp.StatusCode)
		}
	}
}

func TestAnUploadIsRefusedPastTheServiceCeiling(t *testing.T) {
	h := newHarness(t, Options{MaxBytes: 8})
	_, out := h.call(mainToken, "CreateCacheEntry", map[string]string{"key": "k", "version": "v"})
	if st := h.put(out["signed_upload_url"].(string), bytes.Repeat([]byte("x"), 100)); st == http.StatusCreated {
		t.Fatal("an upload past the ceiling was accepted")
	}
	if !h.save(mainToken, "k", "v", []byte("ok")) {
		t.Fatal("a refused upload left its key reserved")
	}
}

func TestNewNeedsAFolder(t *testing.T) {
	if _, err := New(Options{}); err == nil {
		t.Fatal("a service with nowhere to keep entries was built")
	}
}

func TestTheProtobufFieldNamesTheToolkitSendsAreUnderstood(t *testing.T) {
	// Seen from the real client: size_bytes and restore_keys, as protobuf names
	// them. The camelCase spelling is accepted too, and the other tests use it.
	h := newHarness(t, Options{})
	_, out := h.call(mainToken, "CreateCacheEntry", map[string]string{"key": "go-1", "version": "v"})
	h.put(out["signed_upload_url"].(string), []byte("12345"))
	if _, fin := h.call(mainToken, "FinalizeCacheEntryUpload", map[string]string{"key": "go-1", "version": "v", "size_bytes": "5"}); fin["ok"] != true {
		t.Fatalf("size_bytes was not understood: %v", fin)
	}
	_, got := h.call(mainToken, "GetCacheEntryDownloadURL", map[string]any{"key": "go-9", "restore_keys": []string{"go-"}, "version": "v"})
	if got["ok"] != true || got["matched_key"] != "go-1" {
		t.Fatalf("restore_keys was not understood: %v", got)
	}
}

func TestTheEntryIdIsADecimalInteger(t *testing.T) {
	// The toolkit parses it as a BigInt and fails the save on anything else.
	h := newHarness(t, Options{})
	_, out := h.call(mainToken, "CreateCacheEntry", map[string]string{"key": "k", "version": "v"})
	h.put(out["signed_upload_url"].(string), []byte("x"))
	_, fin := h.call(mainToken, "FinalizeCacheEntryUpload", map[string]string{"key": "k", "version": "v", "sizeBytes": "1"})
	id, _ := fin["entry_id"].(string)
	if n, err := strconv.ParseInt(id, 10, 64); err != nil || n < 0 {
		t.Fatalf("entryId %q is not a non-negative int64", id)
	}
}
