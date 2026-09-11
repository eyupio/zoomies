package controller

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/version"
)

// roundTripFunc answers every request with one canned response, which is enough
// for a checker that only ever asks GitHub one question.
type roundTripFunc func(*http.Request) *http.Response

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r), nil }

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(bytes.NewBufferString(body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
}

// withVersion stamps a version for the duration of one test, because the whole
// question this feature answers is "what was this binary built from".
func withVersion(t *testing.T, v string) {
	t.Helper()
	prev := version.Version
	version.Version = v
	t.Cleanup(func() { version.Version = prev })
}

// The distinction the honesty of this feature rests on: a controller built from
// main is stamped with a commit and is normally *ahead* of the newest release,
// so it has nothing to compare and must say nothing.
func TestReleaseVersionAcceptsOnlyABuildFromARelease(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
		ok   bool
	}{
		{"0.2-beta", "0.2-beta", true},
		{"v0.2-beta", "0.2-beta", true},
		{"1.4.0", "1.4.0", true},
		{"v1.4.0", "1.4.0", true},
		// What ci.yml stamps on an image built from main.
		{"main-sha-abc1234", "", false},
		// What the Makefile's git describe gives once commits land on a tag.
		{"v0.2-beta-5-gabc1234", "", false},
		{"v0.2-beta-5-gabc1234-dirty", "", false},
		{"dev", "", false},
		{"", "", false},
	} {
		got, ok := releaseVersion(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("releaseVersion(%q) = %q, %v; want %q, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestDevelopmentCommitAcceptsOnlyPublishedMainBuilds(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
		ok   bool
	}{
		{"main-sha-abc1234", "abc1234", true},
		{"main-sha-0123456789abcdef", "0123456789abcdef", true},
		{"main-sha-short", "", false},
		{"dev", "", false},
		{"1.0.0", "", false},
	} {
		got, ok := developmentCommit(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("developmentCommit(%q) = %q, %v; want %q, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestUpdateProblemSaysNothingWithoutAReleaseToCompare(t *testing.T) {
	h := newHarness(t)

	t.Run("before the first check has answered", func(t *testing.T) {
		withVersion(t, "0.1-alpha")
		if got := h.c.updateProblems(); len(got) != 0 {
			t.Fatalf("problems = %v, want none", got)
		}
	})

	h.c.mu.Lock()
	h.c.release = &releaseState{Tag: "v0.2-beta", URL: "https://example.invalid/r", At: time.Now()}
	h.c.mu.Unlock()

	t.Run("a build from main is ahead, not behind", func(t *testing.T) {
		withVersion(t, "main-sha-abc1234")
		if got := h.c.updateProblems(); len(got) != 0 {
			t.Fatalf("problems = %v, want none: a main build has nothing to compare", got)
		}
	})

	t.Run("already on the current release", func(t *testing.T) {
		withVersion(t, "0.2-beta")
		if got := h.c.updateProblems(); len(got) != 0 {
			t.Fatalf("problems = %v, want none", got)
		}
	})
}

func TestUpdateProblemNamesBothVersions(t *testing.T) {
	h := newHarness(t)
	withVersion(t, "0.1-alpha")
	h.c.mu.Lock()
	h.c.release = &releaseState{Tag: "v0.2-beta", URL: "https://example.invalid/r", At: time.Now()}
	h.c.mu.Unlock()

	got := h.c.updateProblems()
	if len(got) != 1 {
		t.Fatalf("problems = %v, want one", got)
	}
	p := got[0]
	if p.Code != "controller.update_available" {
		t.Fatalf("code = %q", p.Code)
	}
	// Both versions have to appear: the operator decides, and cannot without
	// knowing what they are on as well as what is current.
	if !strings.Contains(p.Title, "v0.2-beta") || !strings.Contains(p.Title, "0.1-alpha") {
		t.Fatalf("title = %q, want both versions named", p.Title)
	}
	if !strings.Contains(p.Fix, "https://example.invalid/r") {
		t.Fatalf("fix = %q, want the release notes linked", p.Fix)
	}
}

func TestDevelopmentUpdateProblemNamesRunningAndMainCommits(t *testing.T) {
	h := newHarness(t)
	withVersion(t, "main-sha-abc1234")
	h.c.mu.Lock()
	h.c.development = &developmentState{SHA: "def5678901234567", URL: "https://example.invalid/commit/def5678", At: time.Now()}
	h.c.mu.Unlock()

	got := h.c.updateProblems()
	if len(got) != 1 {
		t.Fatalf("problems = %v, want one", got)
	}
	p := got[0]
	if p.Code != "controller.development_update_available" || !strings.Contains(p.Title, "abc1234") || !strings.Contains(p.Title, "def5678") {
		t.Fatalf("problem = %+v, want both development commits", p)
	}
	if !strings.Contains(p.Detail, "actually published") || !strings.Contains(p.Fix, "zoomies upgrade") {
		t.Fatalf("problem does not explain the stale channel: %+v", p)
	}

	h.c.mu.Lock()
	h.c.development.SHA = "abc1234fffffffffffffffffffffffff"
	h.c.mu.Unlock()
	if got := h.c.updateProblems(); len(got) != 0 {
		t.Fatalf("matching development build reported as stale: %v", got)
	}
}

// Switching the check off has to switch the notice off too, or an air-gapped
// fleet keeps being told about a release it deliberately stopped asking for.
func TestUpdateProblemIsSilentWhenTheCheckIsOff(t *testing.T) {
	h := newHarness(t)
	withVersion(t, "0.1-alpha")
	h.c.mu.Lock()
	h.c.release = &releaseState{Tag: "v0.2-beta", At: time.Now()}
	h.c.mu.Unlock()

	h.c.live.Update(func(c *config.Config) { c.Updates.CheckInterval = 0 })

	if got := h.c.updateProblems(); len(got) != 0 {
		t.Fatalf("problems = %v, want none", got)
	}
}

func TestCheckForReleaseRecordsWhatGitHubSaid(t *testing.T) {
	h := newHarness(t)
	withVersion(t, "0.1-alpha")
	h.c.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) *http.Response {
		if r.URL.String() != latestReleaseURL {
			t.Errorf("asked %s, want %s", r.URL, latestReleaseURL)
		}
		return jsonResponse(http.StatusOK, `{"tag_name":"v0.2-beta","html_url":"https://example.invalid/r"}`)
	})}

	h.c.checkForRelease(h.ctx)

	got := h.c.latestRelease()
	if got == nil || got.Tag != "v0.2-beta" || got.URL != "https://example.invalid/r" {
		t.Fatalf("release = %+v", got)
	}
}

func TestDevelopmentBuildChecksMainInsteadOfTheLatestRelease(t *testing.T) {
	h := newHarness(t)
	withVersion(t, "main-sha-abc1234")
	h.c.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) *http.Response {
		if r.URL.String() != mainCommitURL {
			t.Errorf("asked %s, want %s", r.URL, mainCommitURL)
		}
		return jsonResponse(http.StatusOK, `{"sha":"def5678901234567","html_url":"https://example.invalid/commit/def5678"}`)
	})}

	h.c.checkForRelease(h.ctx)

	got := h.c.latestDevelopment()
	if got == nil || got.SHA != "def5678901234567" || got.URL != "https://example.invalid/commit/def5678" {
		t.Fatalf("development = %+v", got)
	}
	if got := h.c.latestRelease(); got != nil {
		t.Fatalf("release = %+v, want no release comparison for a main build", got)
	}
}

// A repository with no published release answers 404, and a controller that
// cannot reach GitHub answers nothing. Neither is a problem with the fleet, and
// neither may be mistaken for an answer.
func TestCheckForReleaseKeepsQuietWhenThereIsNoAnswer(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"no release published", http.StatusNotFound, `{"message":"Not Found"}`},
		{"rate limited", http.StatusForbidden, `{"message":"API rate limit exceeded"}`},
		{"an answer with no tag", http.StatusOK, `{}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.c.httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) *http.Response {
				return jsonResponse(tc.status, tc.body)
			})}
			h.c.checkForRelease(h.ctx)
			if got := h.c.latestRelease(); got != nil {
				t.Fatalf("release = %+v, want nothing recorded", got)
			}
		})
	}
}
