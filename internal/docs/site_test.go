package docs

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The partial's own comment explains the attribute it leaves out, so the check
// has to read the markup and not the explanation.
var jinjaComment = regexp.MustCompile(`(?s)\{#.*?#\}`)

// The site's header names the latest release. Material's script used to fill
// it in from the GitHub API and keep the answer in the tab's sessionStorage,
// which a browser restores with the tab: a tab first opened when V1.1.0 was
// the newest release said V1.1.0 two releases later. The facts are resolved
// when the site is built now, and these are the pieces that keep that true.

func TestTheHeaderFactsAreWrittenAtBuildTimeNotFetchedByTheTheme(t *testing.T) {
	partial, err := os.ReadFile("../../overrides/partials/source.html")
	if err != nil {
		t.Fatalf("the source partial override is missing: %v", err)
	}
	markup := jinjaComment.ReplaceAllString(string(partial), "")
	if strings.Contains(markup, "data-md-component") {
		t.Error("overrides/partials/source.html carries data-md-component, so Material's script will mount on it and replace the built facts with whatever the tab cached")
	}
	if !strings.Contains(markup, "md-source__fact--") {
		t.Error("overrides/partials/source.html renders no facts, so the header names no release at all")
	}
	config, err := os.ReadFile("../../mkdocs.yml")
	if err != nil {
		t.Fatalf("reading mkdocs.yml: %v", err)
	}
	if !strings.Contains(string(config), "- hooks/source.py") {
		t.Error("mkdocs.yml does not register hooks/source.py, so nothing resolves the facts the partial renders")
	}
}

func TestTheSiteIsRebuiltAndPublishedWhenAReleaseIsPublished(t *testing.T) {
	body := workflowFiles(t)["docs.yml"]
	if body == "" {
		t.Fatal("docs.yml is missing")
	}
	for _, want := range []struct {
		needle, why string
	}{
		{"release:\n    types: [published]", "a release that does not rebuild the site leaves the header naming the one before"},
		{"github.event_name == 'release' && 'main'", "a release event runs at its tag, and the site is main's docs"},
		{"github.event_name == 'release')", "the deploy job only publishes from main, and a release event's ref is its tag"},
		{"GITHUB_TOKEN: ${{ github.token }}", "anonymous API requests share a rate limit a shared runner's address can already have spent"},
		{`grep -q "md-source__fact--version" site/index.html`, "an offline build is allowed to succeed without the facts; the published site is not"},
	} {
		if !strings.Contains(body, want.needle) {
			t.Errorf("docs.yml has no %q: %s", want.needle, want.why)
		}
	}
}
