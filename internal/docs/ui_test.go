package docs

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The pages of the UI, and the two places that count them.
const (
	sectionsSource = "../../web/src/lib/shell/sections.ts"
	uiTour         = "../../docs/ui.md"
)

// The front pages say how many pages the UI has, and the number has to be
// true.
//
// It is the first concrete claim a reader meets on both the README and the
// site's home page, and it is the kind of number nobody thinks to revisit:
// Usage and Providers were both added without it moving, so it sat at ten
// while the navigation grew to twelve. A count that is quietly wrong in the
// first sentence is a poor argument for trusting the rest.
func TestTheFrontPagesCountThePagesTheUIHas(t *testing.T) {
	body, err := os.ReadFile(sectionsSource)
	if err != nil {
		t.Fatalf("reading %s: %v", sectionsSource, err)
	}
	// The entries of SECTIONS, which is the one list the sidebar, the phone's
	// bar and its menu all read -- so counting it counts what an operator sees.
	entries := regexp.MustCompile(`\{\s*path: '[^']*', label: '`).FindAllString(string(body), -1)
	if len(entries) < 2 {
		t.Fatalf("found %d sections in %s; the list moved rather than the count", len(entries), sectionsSource)
	}
	want := numberWord(len(entries))
	if want == "" {
		t.Fatalf("the UI has %d sections and this test cannot spell that; add the word", len(entries))
	}

	// Both front doors make the claim, in their own words, so the pattern is
	// the number followed by "pages" rather than a sentence to match.
	claim := regexp.MustCompile(`(?i)\b([a-z]+) pages, one job each\b`)
	for _, page := range []string{"../../README.md", "../../docs/index.md"} {
		text, err := os.ReadFile(page)
		if err != nil {
			t.Fatalf("reading %s: %v", page, err)
		}
		found := claim.FindStringSubmatch(string(text))
		if found == nil {
			t.Errorf("%s no longer says how many pages the UI has; if that claim moved, move this test with it", page)
			continue
		}
		if !strings.EqualFold(found[1], want) {
			t.Errorf("%s says %q pages; the navigation has %d, so it should say %q",
				page, found[1], len(entries), want)
		}
	}
}

// Every page in the navigation is described somewhere in the tour.
//
// docs/ui.md is linked from both front pages as "see every page", which is a
// promise rather than a title: a page that is in the product and not in the
// tour is one a reader concludes does not exist.
func TestTheTourDescribesEveryPageInTheNavigation(t *testing.T) {
	body, err := os.ReadFile(sectionsSource)
	if err != nil {
		t.Fatalf("reading %s: %v", sectionsSource, err)
	}
	labels := regexp.MustCompile(`label: '([^']+)'`).FindAllStringSubmatch(string(body), -1)
	if len(labels) < 2 {
		t.Fatalf("found %d labels in %s; the list moved", len(labels), sectionsSource)
	}

	tour, err := os.ReadFile(uiTour)
	if err != nil {
		t.Fatalf("reading %s: %v", uiTour, err)
	}
	headings := map[string]bool{}
	for _, line := range strings.Split(string(tour), "\n") {
		if after, ok := strings.CutPrefix(line, "## "); ok {
			headings[strings.ToLower(strings.TrimSpace(after))] = true
		}
	}

	var missing []string
	for _, label := range labels {
		if !headings[strings.ToLower(label[1])] {
			missing = append(missing, label[1])
		}
	}
	if len(missing) > 0 {
		t.Errorf("these pages are in the navigation but have no section in %s: %s",
			uiTour, strings.Join(missing, ", "))
	}
}

// numberWord spells the counts a navigation could plausibly have. The front
// pages write the number as a word because they are prose, so the test has to
// compare words.
func numberWord(n int) string {
	words := map[int]string{
		6: "Six", 7: "Seven", 8: "Eight", 9: "Nine", 10: "Ten", 11: "Eleven",
		12: "Twelve", 13: "Thirteen", 14: "Fourteen", 15: "Fifteen", 16: "Sixteen",
	}
	return words[n]
}
