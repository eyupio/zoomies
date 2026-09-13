package migrate

import (
	"strings"
	"testing"
)

// The badge joins a row of badges that is already there, because that is where
// a maintainer would have put it, and the pull request is then one inserted
// line in a block they already own.
func TestAddBadgeJoinsAnExistingRowOfBadges(t *testing.T) {
	in := "# Widgets\n\n[![CI](https://x/ci.svg)](https://x)\n![Licence](https://x/l.svg)\n\nWidgets does things.\n"
	got, changed := AddBadge(in)
	if !changed {
		t.Fatal("AddBadge changed nothing")
	}
	want := "# Widgets\n\n[![CI](https://x/ci.svg)](https://x)\n![Licence](https://x/l.svg)\n" + BadgeMarkdown + "\n\nWidgets does things.\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// Without a row to join it sits under the title as a paragraph of its own,
// rather than glued to the heading or to the first line of prose.
func TestAddBadgeSitsUnderTheTitle(t *testing.T) {
	cases := map[string]struct{ in, want string }{
		"atx title then prose": {
			"# Widgets\nWidgets does things.\n",
			"# Widgets\n\n" + BadgeMarkdown + "\n\nWidgets does things.\n",
		},
		"atx title then blank": {
			"# Widgets\n\nWidgets does things.\n",
			"# Widgets\n\n" + BadgeMarkdown + "\n\nWidgets does things.\n",
		},
		"setext title": {
			"Widgets\n=======\n\nProse.\n",
			"Widgets\n=======\n\n" + BadgeMarkdown + "\n\nProse.\n",
		},
		"title after a centred logo block": {
			"<div align=\"center\">\n<img src=\"logo.png\">\n</div>\n\n# Widgets\n\nProse.\n",
			"<div align=\"center\">\n<img src=\"logo.png\">\n</div>\n\n# Widgets\n\n" + BadgeMarkdown + "\n\nProse.\n",
		},
		"title after front matter": {
			"---\ntitle: Widgets\n---\n# Widgets\n\nProse.\n",
			"---\ntitle: Widgets\n---\n# Widgets\n\n" + BadgeMarkdown + "\n\nProse.\n",
		},
		"title only": {
			"# Widgets\n",
			"# Widgets\n\n" + BadgeMarkdown + "\n",
		},
		"a heading inside a code fence is not the title": {
			"```\n# not a title\n```\n\n# Widgets\n\nProse.\n",
			"```\n# not a title\n```\n\n# Widgets\n\n" + BadgeMarkdown + "\n\nProse.\n",
		},
		"no title at all": {
			"Widgets does things.\n",
			BadgeMarkdown + "\n\nWidgets does things.\n",
		},
		"empty": {
			"",
			BadgeMarkdown + "\n",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, changed := AddBadge(tc.in)
			if !changed {
				t.Fatal("AddBadge changed nothing")
			}
			if got != tc.want {
				t.Errorf("got:\n%q\nwant:\n%q", got, tc.want)
			}
		})
	}
}

// A CRLF README must come back CRLF throughout: a one-line change that also
// converts every line ending is a diff nobody can review.
func TestAddBadgeKeepsTheFilesLineEndings(t *testing.T) {
	got, _ := AddBadge("# Widgets\r\n\r\nProse.\r\n")
	if strings.Contains(strings.ReplaceAll(got, "\r\n", ""), "\n") {
		t.Errorf("a bare LF crept in: %q", got)
	}
	if !strings.Contains(got, "\r\n"+BadgeMarkdown+"\r\n") {
		t.Errorf("badge not on a CRLF line: %q", got)
	}
}

// The wizard can be run twice against the same repository, and a maintainer
// may have moved or reworded the badge. Recognising it by URL is what stops
// a second copy appearing.
func TestAddBadgeNeverAddsASecond(t *testing.T) {
	in := "# Widgets\n\n| [![runs on Zoomies](" + BadgeURL + ")](https://zoomies.sh) |\n"
	got, changed := AddBadge(in)
	if changed || got != in {
		t.Errorf("changed=%v got=%q", changed, got)
	}
}

func TestIsMarkdownReadme(t *testing.T) {
	for p, want := range map[string]bool{
		"README.md": true, "readme.markdown": true, "docs/README.MD": true,
		"README.rst": false, "README.txt": false, "README": false, "": false,
	} {
		if got := IsMarkdownReadme(p); got != want {
			t.Errorf("IsMarkdownReadme(%q) = %v, want %v", p, got, want)
		}
	}
}

// A row of badges is recognised by shape, so a line of prose that happens to
// start with an image is not mistaken for one.
func TestIsBadgeLine(t *testing.T) {
	for line, want := range map[string]bool{
		"[![CI](a)](b)":               true,
		"![CI](a) [![X](b)](c)":       true,
		"  ![CI](a)  ":                true,
		"![CI](a) is our build badge": false,
		"See ![CI](a)":                false,
		"[![CI](a)":                   false,
		"":                            false,
	} {
		if got := isBadgeLine(line); got != want {
			t.Errorf("isBadgeLine(%q) = %v, want %v", line, got, want)
		}
	}
}
