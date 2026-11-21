package installer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/eyupio/zoomies/internal/backup"
	"github.com/eyupio/zoomies/internal/releasekey"
	"github.com/eyupio/zoomies/internal/version"
)

// ErrSignature is a release whose checksums are not signed by a key this build
// trusts, or whose signature does not verify. Nothing is downloaded or
// installed after it: the checksums travel with the binary, so on their own
// they say nothing about who made either.
var ErrSignature = errors.New("the release is not signed by a trusted release key")

// SelfUpdateEnv, when set, stops `zoomies upgrade` fetching a binary. install.sh
// sets it because it has just installed one, and the re-executed process sets
// it so that a binary which still looks out of date to itself cannot loop. It
// is an environment variable rather than a flag so a release that predates it
// ignores it instead of refusing an unknown flag.
const SelfUpdateEnv = "ZOOMIES_NO_SELF_UPDATE"

// KeptPreviousEnv carries, across the re-execution into the binary a self-update
// just installed, the SHA-256 of the previous binary that self-update kept
// beside it. The run that waits for the service puts that file back when the
// service never answers, and has to be sure the file is the one this upgrade
// kept: after an upgrade that did not go through the self-update (install.sh
// --upgrade, --no-download) the file left is from an earlier one, and a database
// a newer release has migrated cannot be opened by it. Like SelfUpdateEnv it is
// an environment variable, so a release that predates it ignores it.
const KeptPreviousEnv = "ZOOMIES_UPGRADE_KEPT_PREVIOUS"

// KeptPreviousMarker is the environment entry that names the previous binary
// kept beside binary, or "" when there is none or it cannot be read, in which
// case nothing is rolled back to: that is the safe side.
func KeptPreviousMarker(binary string) string {
	sum, err := backup.FileSHA256(binary + PreviousSuffix)
	if err != nil {
		return ""
	}
	return KeptPreviousEnv + "=" + sum
}

// SelfUpdateOptions says which binary to bring up to date and from where.
type SelfUpdateOptions struct {
	// BinaryPath is the binary the service runs, which is what gets replaced.
	BinaryPath string
	// Version pins a tag, or "dev" for the rolling build. Empty follows the
	// channel this binary was built from: the newest release, or dev.
	Version string
	// Current is the version this binary reports, for the downgrade guard.
	Current string
	// BaseURL is the releases URL; install.sh's ZOOMIES_BASE_URL and
	// ZOOMIES_REPO decide it when empty.
	BaseURL string
	Out     io.Writer
	Client  *http.Client
	// Check only says what would change.
	Check bool
	// DryRun says what a real run would do and installs nothing: the release
	// is still fetched, verified and probed, and the pre-flight still runs, so
	// a preview fails for the same reasons an update would. It takes no lock,
	// replaces no binary and keeps no previous one, so nothing beside the
	// installed binary is changed.
	DryRun bool
	// Preflight, when set, is asked about the downloaded file after it has been
	// verified and before it replaces anything. An error stops the update with
	// the installed binary untouched.
	Preflight func(ctx context.Context, candidate string) error
	// ReleaseKey is the set of public keys a release's checksums signature is
	// held to, one base64 key per line, so more than one is trusted while a key
	// is rotated. Empty is no key: the checksums are taken on trust, and the
	// download says so. A dev build, which is whatever main built last and has
	// no release to sign, is not held to a signature either.
	ReleaseKey string

	goos, goarch string
	run          commandRunner
}

// SelfUpdateResult says what was found and what was done about it.
type SelfUpdateResult struct {
	Tag     string
	Updated bool
	// Newer is set by a check when a different build is available.
	Newer bool
}

func (o *SelfUpdateOptions) defaults() {
	if o.Out == nil {
		o.Out = io.Discard
	}
	if o.Client == nil {
		o.Client = &http.Client{Timeout: 5 * time.Minute}
	}
	if o.goos == "" {
		o.goos = runtime.GOOS
	}
	if o.goarch == "" {
		o.goarch = runtime.GOARCH
	}
	if o.run == nil {
		o.run = runCommand
	}
	if o.BaseURL == "" {
		o.BaseURL = os.Getenv("ZOOMIES_BASE_URL")
	}
	if o.BaseURL == "" {
		repo := os.Getenv("ZOOMIES_REPO")
		if repo == "" {
			repo = "eyupio/zoomies"
		}
		o.BaseURL = "https://github.com/" + repo + "/releases"
	}
	o.BaseURL = strings.TrimRight(o.BaseURL, "/")
}

// SelfUpdate replaces the installed binary with the newest published one when
// it differs, and refuses anything it could not verify. It is the download half
// of install.sh, so `zoomies upgrade` on its own leaves a host on the current
// release rather than restarting the build it already had.
//
// It is fetch, then the optional pre-flight, then install, and the three are
// separate so that the downloaded release can be asked about this deployment
// while the binary that is known to work is still the one in place.
func SelfUpdate(ctx context.Context, opts SelfUpdateOptions) (SelfUpdateResult, error) {
	opts.defaults()
	cand, res, err := FetchRelease(ctx, opts)
	if err != nil || cand == nil {
		return res, err
	}
	defer cand.Discard()
	if opts.Preflight != nil {
		if err := opts.Preflight(ctx, cand.Path); err != nil {
			return res, fmt.Errorf("the downloaded %s does not accept this deployment, so the installed binary was left in place: %w", cand.Tag, err)
		}
	}
	// The preview has done everything a real run does up to the swap, and the
	// caller reads res to say what would happen. res.Updated stays false: no
	// binary was replaced.
	if opts.DryRun {
		return res, nil
	}
	if err := cand.Install(opts); err != nil {
		return res, err
	}
	res.Updated = true
	return res, nil
}

// Candidate is a release that has been downloaded, hashed against the
// published checksums and started once, and that has replaced nothing. Its
// owner either installs it or discards it.
type Candidate struct {
	Tag string
	// Path is the temporary file beside the installed binary, so that the
	// rename in Install stays on one filesystem.
	Path string
}

// FetchRelease is everything SelfUpdate does short of replacing the binary:
// choose the tag, apply the downgrade guard, compare hashes, download to a
// temporary file, verify the checksum and run `version --short` on it. The
// checksum is verified before the file is executed, and the installed binary
// is not touched. The Candidate is nil when nothing newer was found, and
// always when opts.Check is set.
//
// Whether a build is out of date is decided by hashing the installed file
// against the release's checksums.txt, not by comparing version strings: a
// rolling dev build carries no version worth comparing, and a hash cannot be
// fooled by two builds of one tag.
func FetchRelease(ctx context.Context, opts SelfUpdateOptions) (*Candidate, SelfUpdateResult, error) {
	opts.defaults()
	var res SelfUpdateResult
	if opts.goos != "linux" && opts.goos != "darwin" {
		return nil, res, nil
	}
	tag, err := opts.target(ctx)
	if err != nil {
		return nil, res, err
	}
	res.Tag = tag
	if tag == "" {
		return nil, res, nil
	}
	if tag != "dev" && opts.Current != "" && version.CompareBuilds(opts.Current, tag) == version.SkewAhead {
		// Never turn `upgrade` into a downgrade: an older build may not read
		// a database a newer one has migrated.
		return nil, res, nil
	}
	asset := "zoomies_" + opts.goos + "_" + opts.goarch
	sums, err := opts.fetchText(ctx, opts.BaseURL+"/download/"+tag+"/checksums.txt")
	if err != nil {
		return nil, res, fmt.Errorf("could not fetch the checksums for %s, so no binary was downloaded: %w", tag, err)
	}
	if err := opts.verifyChecksums(ctx, tag, sums); err != nil {
		return nil, res, err
	}
	want := checksumFor(sums, asset)
	if want == "" {
		return nil, res, fmt.Errorf("the checksums for %s have no entry for %s, so no binary was downloaded", tag, asset)
	}
	have, err := backup.FileSHA256(opts.BinaryPath)
	if err != nil {
		return nil, res, fmt.Errorf("read the installed binary: %w", err)
	}
	if have == want {
		return nil, res, nil
	}
	res.Newer = true
	if opts.Check {
		return nil, res, nil
	}
	// A preview installs nothing, so its download need not be on the same
	// filesystem as the binary, and an operator previewing as a service
	// account may not be able to write beside it. It goes to the temporary
	// directory instead, which is why --dry-run never touches /usr/local/bin.
	dir := filepath.Dir(opts.BinaryPath)
	if opts.DryRun {
		dir = ""
	}
	tmp, err := os.CreateTemp(dir, ".zoomies-update-*")
	if err != nil {
		return nil, res, fmt.Errorf("cannot write to %s: %w; run the upgrade as root (sudo zoomies upgrade), or use install.sh --upgrade --prefix for another directory", dir, err)
	}
	cand := &Candidate{Tag: tag, Path: tmp.Name()}
	got, err := opts.download(ctx, opts.BaseURL+"/download/"+tag+"/"+asset, tmp)
	_ = tmp.Close()
	if err != nil {
		cand.Discard()
		return nil, res, fmt.Errorf("download %s %s: %w", asset, tag, err)
	}
	if got != want {
		cand.Discard()
		return nil, res, fmt.Errorf("checksum mismatch for %s %s (expected %s, got %s); the installed binary was left in place. Try again, and report it if it happens twice", asset, tag, want[:12], got[:12])
	}
	if err := os.Chmod(cand.Path, 0o755); err != nil {
		cand.Discard()
		return nil, res, err
	}
	// A file that hashes right but will not run -- the wrong architecture
	// behind a matching sums file on a mirror -- must not replace one that does.
	reported, err := opts.run(ctx, cand.Path, "version", "--short")
	if err != nil {
		cand.Discard()
		return nil, res, fmt.Errorf("the downloaded binary will not run on this host; the installed one was left in place: %w", err)
	}
	// Nor may one that is a different release from the one asked for. The
	// signature covers the checksums, which name hashes and assets and no
	// version, so a mirror (or replaced release assets) could serve an old
	// release's genuine checksums, signature and binary under a newer tag, and
	// the downgrade guard above would see only the tag. What the binary says it
	// is does not come from the same place. A release reports its version
	// without the leading v, which CompareBuilds allows for; the rolling build
	// reports whatever it was called, so it is not held to a tag. An empty
	// answer is refused, because CompareBuilds takes it as nothing to compare.
	if tag != "dev" {
		name, _, _ := strings.Cut(strings.TrimSpace(reported), " ")
		if name == "" || version.CompareBuilds(name, tag) != version.SkewNone {
			cand.Discard()
			return nil, res, fmt.Errorf("the download for %s reports itself as %q, so it is not the release that was asked for; the installed binary was left in place",
				tag, strings.TrimSpace(reported))
		}
	}
	return cand, res, nil
}

// PreviousSuffix is what the binary it replaced is kept under, beside it.
const PreviousSuffix = ".previous"

// linkFile is a variable so a test can stand in for a filesystem that refuses
// hard links.
var linkFile = os.Link

// Install puts the candidate in place of opts.BinaryPath and keeps what was
// there as BinaryPath+".previous", replacing any earlier one, so a manual
// rollback has a binary to go back to.
//
// The old binary is hard-linked where the filesystem allows it: the link names
// the old inode, which survives the rename that follows, and costs no second
// copy of a file that may be large. Where a link cannot be made it is copied
// instead, with its mode, because a rollback file that is a copy is still one.
// Either is made under a temporary name and renamed over any earlier
// .previous, so a failure before that rename leaves the installed binary and
// the last .previous as they were. The two renames are not one step: if the
// final rename of the candidate fails, .previous is already the binary that was
// running and the older rollback file has been replaced by it.
//
// If the old binary can be kept neither way, nothing is replaced.
func (c *Candidate) Install(opts SelfUpdateOptions) error {
	previous := opts.BinaryPath + PreviousSuffix
	staged := previous + ".new"
	_ = os.Remove(staged)
	if linkErr := linkFile(opts.BinaryPath, staged); linkErr != nil {
		_ = os.Remove(staged)
		if err := copyBinary(opts.BinaryPath, staged); err != nil {
			_ = os.Remove(staged)
			// Both are named: the copy's error alone would hide that a link was tried
			// first, and why it was not enough.
			return fmt.Errorf("keep %s as %s: it could not be linked (%v) or copied (%w); the installed binary was left in place", opts.BinaryPath, previous, linkErr, err)
		}
	}
	if err := os.Rename(staged, previous); err != nil {
		_ = os.Remove(staged)
		return fmt.Errorf("keep %s as %s: %w; the installed binary was left in place", opts.BinaryPath, previous, err)
	}
	if err := os.Rename(c.Path, opts.BinaryPath); err != nil {
		return fmt.Errorf("replace %s: %w", opts.BinaryPath, err)
	}
	return nil
}

// restorePreviousBinary puts the binary kept beside the installed one back in
// its place, through a staging file and a rename so the path is never half
// written. It is what an upgrade rolls back to when the new release does not
// answer and ran no migration.
func restorePreviousBinary(binary string) error {
	previous := binary + PreviousSuffix
	staged := binary + ".rollback"
	_ = os.Remove(staged)
	if err := copyBinary(previous, staged); err != nil {
		_ = os.Remove(staged)
		return fmt.Errorf("reading the previous binary %s: %w", previous, err)
	}
	if err := os.Rename(staged, binary); err != nil {
		_ = os.Remove(staged)
		return fmt.Errorf("putting %s back: %w", binary, err)
	}
	return nil
}

// copyBinary copies src to dst with src's mode. It is only the fallback for a
// filesystem that cannot hard-link, so it does not try to be atomic: dst is a
// staging name that Install renames or removes.
func copyBinary(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	// The creation mode is subject to the umask; the rollback file should run
	// exactly as the binary did.
	return os.Chmod(dst, info.Mode().Perm())
}

// Discard removes the temporary file. It is safe after Install, when the file
// has already become the installed binary and there is nothing at Path, and on
// a nil Candidate, so it can be deferred before the fetch is known to have
// produced one.
func (c *Candidate) Discard() {
	if c == nil {
		return
	}
	_ = os.Remove(c.Path)
}

// verifyChecksums holds a release's checksums.txt to the release key, when one
// is built in, before any digest in it is trusted. The checksums travel with
// the binary, so without this they say only that a download was not corrupted
// on the way, not who made it.
func (o *SelfUpdateOptions) verifyChecksums(ctx context.Context, tag, sums string) error {
	keys := strings.TrimSpace(o.ReleaseKey)
	if keys == "" {
		fmt.Fprintf(o.Out, "This build has no release key built in, so %s is not held to a signature; its checksums are taken on trust (docs/upgrading.md#signing-releases).\n", tag)
		return nil
	}
	if tag == "dev" {
		// dev is whatever main built last: a moving build with no release to
		// sign, so there is no signature to hold it to.
		return nil
	}
	sigAsset := "checksums.txt" + releasekey.SigSuffix
	sig, err := o.fetchText(ctx, o.BaseURL+"/download/"+tag+"/"+sigAsset)
	if err != nil {
		return fmt.Errorf("%w: %s carries no %s: %w. Nothing was downloaded", ErrSignature, tag, sigAsset, err)
	}
	if err := releasekey.Verify(keys, []byte(sums), []byte(sig)); err != nil {
		return fmt.Errorf("%w: %v. Nothing was downloaded; do not install this release, and report it if it happens twice", ErrSignature, err)
	}
	return nil
}

// target is the tag to follow: the one asked for, the rolling dev build for a
// binary that came from main, or the newest release.
func (o *SelfUpdateOptions) target(ctx context.Context) (string, error) {
	if v := strings.TrimSpace(o.Version); v != "" {
		if v == "dev" || strings.HasPrefix(v, "v") {
			return v, nil
		}
		return "v" + v, nil
	}
	channel, ok := version.InstallTag(o.Current)
	switch {
	case !ok:
		// A local build has no channel to follow, and replacing it with a
		// published one would throw away whatever the developer is testing.
		return "", nil
	case channel == "dev":
		return "dev", nil
	}
	return o.latestTag(ctx)
}

// latestTag reads the tag from the /releases/latest redirect, which spends no
// API quota, and falls back to the release list for the period in which every
// release is a pre-release and the redirect names no tag.
func (o *SelfUpdateOptions) latestTag(ctx context.Context) (string, error) {
	client := *o.Client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.BaseURL+"/latest", nil)
	if err == nil {
		req.Header.Set("User-Agent", version.UserAgent())
		if resp, err := client.Do(req); err == nil {
			_ = resp.Body.Close()
			if loc := resp.Header.Get("Location"); strings.Contains(loc, "/tag/") {
				return loc[strings.LastIndex(loc, "/tag/")+len("/tag/"):], nil
			}
		}
	}
	// Only github.com has this API shape; a mirror gets the error instead of a
	// guess at what its API might be.
	const prefix = "https://github.com/"
	if !strings.HasPrefix(o.BaseURL, prefix) || !strings.HasSuffix(o.BaseURL, "/releases") {
		return "", fmt.Errorf("could not work out the latest release from %s; pass --version v1.2.3", o.BaseURL)
	}
	repo := strings.TrimSuffix(strings.TrimPrefix(o.BaseURL, prefix), "/releases")
	body, err := o.fetchText(ctx, "https://api.github.com/repos/"+repo+"/releases?per_page=20")
	if err != nil {
		return "", fmt.Errorf("could not work out the latest release: %w; pass --version v1.2.3", err)
	}
	var list []struct {
		Tag string `json:"tag_name"`
	}
	if err := json.Unmarshal([]byte(body), &list); err == nil {
		for _, r := range list {
			if strings.HasPrefix(r.Tag, "v") {
				return r.Tag, nil
			}
		}
	}
	return "", fmt.Errorf("could not work out the latest release; pass --version v1.2.3")
}

func (o *SelfUpdateOptions) fetchText(ctx context.Context, u string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", version.UserAgent())
	resp, err := o.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s answered %s", redact(u), resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return string(b), err
}

// download streams the body to w and returns its SHA-256, so the hash is of
// what was written rather than of a second read.
func (o *SelfUpdateOptions) download(ctx context.Context, u string, w io.Writer) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", version.UserAgent())
	resp, err := o.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s answered %s", redact(u), resp.Status)
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(w, h), resp.Body); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func redact(u string) string {
	if p, err := url.Parse(u); err == nil {
		p.User, p.RawQuery = nil, ""
		return p.String()
	}
	return u
}

// checksumFor finds an asset's hash in a `sha256sum` style file.
func checksumFor(sums, asset string) string {
	for _, line := range strings.Split(sums, "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && (f[1] == asset || f[1] == "*"+asset) {
			return strings.ToLower(f[0])
		}
	}
	return ""
}
