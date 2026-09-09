package controller

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/eyupio/zoomies/internal/version"
)

// The release check asks github.com which release of Zoomies is current, so an
// operator learns that their controller is behind from the UI rather than from
// a changelog they were not reading.
//
// It is deliberately the only thing in Zoomies that talks to github.com
// regardless of github.api_base_url: the releases of this software live there
// whichever GitHub a fleet is pointed at. An Enterprise Server deployment with
// no route to github.com should switch it off, and updates.check_interval: 0
// says so plainly.
const (
	// latestReleaseURL is the API redirect GitHub maintains for the newest
	// release that is neither a draft nor a prerelease.
	latestReleaseURL = "https://api.github.com/repos/eyupio/zoomies/releases/latest"
	// updateCheckTimeout bounds the request. Nothing waits on this, so it can
	// afford to be patient, but not to hold a housekeeping pass open.
	updateCheckTimeout = 15 * time.Second
)

// releaseState is what the last successful check learned.
type releaseState struct {
	// Tag is the release's tag, e.g. "v0.2-beta".
	Tag string
	// URL is its release page, so the UI can link to the notes.
	URL string
	// At is when this was learned, so a stale answer can be recognised.
	At time.Time
}

// releaseLike matches a version stamped from a release tag: 0.2-beta, v1.4.0.
// A build from main is stamped main-sha-abc1234 and matches nothing here.
var releaseLike = regexp.MustCompile(`^v?\d+\.\d+`)

// describeSuffix matches what `git describe` adds to a tag once commits have
// landed on top of it -- v0.2-beta-5-gabc1234 -- which is a local build of
// something past that release rather than the release itself.
var describeSuffix = regexp.MustCompile(`-\d+-g[0-9a-f]{7,}(-dirty)?$`)

// releaseVersion reports the release this binary was built from, and whether it
// was built from one at all.
//
// This is the whole honesty of the feature. A controller running :latest or
// :main is stamped main-sha-abc1234 and is usually *ahead* of the newest
// release, so telling it that a release is available would be telling it to
// downgrade. Only a build that came from a release tag has anything to compare.
func releaseVersion(v string) (string, bool) {
	v = strings.TrimSpace(v)
	if v == "" || !releaseLike.MatchString(v) || describeSuffix.MatchString(v) {
		return "", false
	}
	return strings.TrimPrefix(v, "v"), true
}

// checkForRelease asks GitHub for the current release and records it.
//
// A failure is logged at debug and otherwise ignored: a controller that cannot
// reach github.com is not a controller with a problem, and the previous answer
// stays until a later pass replaces it.
func (c *Controller) checkForRelease(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, updateCheckTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseURL, nil)
	if err != nil {
		c.log.Debug("could not build the release check request", "error", err)
		return
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", version.UserAgent())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.log.Debug("could not ask GitHub which release is current", "error", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// 404 is what a repository with no published release answers, and it is
		// not a failure: there is simply nothing to compare against yet.
		c.log.Debug("the release check was refused", "status", resp.StatusCode)
		return
	}

	var body struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		c.log.Debug("could not read the release check answer", "error", err)
		return
	}
	if strings.TrimSpace(body.TagName) == "" {
		return
	}

	c.mu.Lock()
	c.release = &releaseState{Tag: body.TagName, URL: body.HTMLURL, At: c.Now()}
	c.mu.Unlock()
}

// latestRelease returns what the last check learned, or nil.
func (c *Controller) latestRelease() *releaseState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.release
}
