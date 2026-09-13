package migrate

import (
	"path"
	"strings"
)

// The badge: the one line a migration adds outside .github/workflows.
//
// A repository whose CI runs on Zoomies gets to say so, in its README, with a
// badge served from zoomies.sh -- the same way it says which licence it is
// under and whether its build is green. It is how the people who read that
// README find out Zoomies exists, which is why the wizard adds it by default;
// it is also somebody else's README, which is why it is one line in a block
// they already have, is never added twice, and is a checkbox away from not
// being added at all.

const (
	// BadgeURL is where the badge is served from. The site copies it from
	// docs/badge.svg, so the file a contributor edits is the one every README
	// shows.
	BadgeURL = "https://zoomies.sh/badge.svg"
	// BadgeLink is where clicking the badge goes.
	BadgeLink = "https://zoomies.sh"
	// BadgeAlt is the badge's alternative text: what a screen reader says and
	// what a README shows when the image cannot load.
	BadgeAlt = "CI has the Zoomies"
	// BadgeMarkdown is the line itself.
	BadgeMarkdown = "[![" + BadgeAlt + "](" + BadgeURL + ")](" + BadgeLink + ")"
)

// HasBadge reports whether a README already carries the badge, by URL rather
// than by the exact line: a maintainer who reworded the alt text or wrapped
// the badge in their own table has it, and must not be offered it again.
func HasBadge(readme string) bool {
	return strings.Contains(readme, BadgeURL)
}

// IsMarkdownReadme reports whether a README's path is one the badge line can
// be written into. A README in reStructuredText or plain text is left alone:
// Markdown image syntax in the middle of it is a broken line, not a badge.
func IsMarkdownReadme(p string) bool {
	switch strings.ToLower(path.Ext(strings.TrimSpace(p))) {
	case ".md", ".markdown", ".mdown", ".mkd", ".mkdn":
		return true
	}
	return false
}

// AddBadge returns the README with the badge in it, and whether anything
// changed. A README that already carries the badge comes back untouched.
//
// Where it goes is decided the way a maintainer would decide it. A README
// that already has a row of badges -- the licence, the build, the release --
// gets this one at the end of that row, because that is where badges live.
// One without such a row gets it on its own line under the title, or at the
// very top when there is no title to sit under. Nothing else moves: the edit
// is a single inserted line so the diff in the pull request is one line, and
// the file's own line endings are kept so a CRLF README does not come back
// half converted.
func AddBadge(readme string) (string, bool) {
	if HasBadge(readme) {
		return readme, false
	}
	eol := "\n"
	if strings.Contains(readme, "\r\n") {
		eol = "\r\n"
	}
	lines := strings.Split(readme, eol)
	at, own := badgePosition(lines)

	var out []string
	out = append(out, lines[:at]...)
	if own {
		// On its own line, with a blank line either side so it is a paragraph
		// of its own rather than glued to the heading or the prose.
		if at > 0 && strings.TrimSpace(lines[at-1]) != "" {
			out = append(out, "")
		}
		out = append(out, BadgeMarkdown)
		if at >= len(lines) || strings.TrimSpace(lines[at]) != "" {
			out = append(out, "")
		}
	} else {
		out = append(out, BadgeMarkdown)
	}
	out = append(out, lines[at:]...)
	return strings.Join(out, eol), true
}

// badgePosition finds the line index the badge is inserted before, and
// whether it needs a paragraph of its own (true) or joins a row of badges
// already there (false).
//
// The search is bounded to the first heading's neighbourhood: badges under
// the title are a convention, badges under the third section are not, and a
// row of images halfway down a README is more likely a gallery.
func badgePosition(lines []string) (int, bool) {
	// Skip front matter and any leading HTML block -- a centred logo, say --
	// to find the title. Both are common at the top of a README and both are
	// wrong places for a line of Markdown.
	i := 0
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		for j := 1; j < len(lines); j++ {
			if strings.TrimSpace(lines[j]) == "---" {
				i = j + 1
				break
			}
		}
	}
	title := -1
	inFence := false
	for j := i; j < len(lines); j++ {
		t := strings.TrimSpace(lines[j])
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if strings.HasPrefix(t, "# ") || t == "#" {
			title = j
			break
		}
		// A setext title: a line of text with a row of = under it.
		if j+1 < len(lines) && t != "" && !strings.HasPrefix(t, "<") && isSetextUnderline(lines[j+1]) {
			title = j + 1
			break
		}
	}
	if title < 0 {
		// No title anywhere: the badge opens the file, after any front matter.
		return badgeRowEnd(lines, i)
	}
	return badgeRowEnd(lines, title+1)
}

// badgeRowEnd looks from a line for a row of badges -- consecutive lines that
// are images or linked images, after any blank lines -- and answers the index
// after the last of them, or the starting index when there is no such row.
func badgeRowEnd(lines []string, from int) (int, bool) {
	j := from
	for j < len(lines) && strings.TrimSpace(lines[j]) == "" {
		j++
	}
	if j >= len(lines) || !isBadgeLine(lines[j]) {
		return from, true
	}
	for j < len(lines) && isBadgeLine(lines[j]) {
		j++
	}
	return j, false
}

// isBadgeLine is a line that is nothing but Markdown images, linked or not:
// the shape every badge row takes.
func isBadgeLine(line string) bool {
	t := strings.TrimSpace(line)
	if t == "" {
		return false
	}
	for t != "" {
		t = strings.TrimLeft(t, " ")
		var ok bool
		if strings.HasPrefix(t, "[![") {
			t, ok = cutImageLink(t)
		} else if strings.HasPrefix(t, "![") {
			t, ok = cutImage(t)
		} else {
			return false
		}
		if !ok {
			return false
		}
	}
	return true
}

// cutImage removes a leading ![alt](src) and reports whether one was there.
func cutImage(t string) (string, bool) {
	rest, ok := strings.CutPrefix(t, "![")
	if !ok {
		return t, false
	}
	close := strings.Index(rest, "](")
	if close < 0 {
		return t, false
	}
	end := strings.IndexByte(rest[close:], ')')
	if end < 0 {
		return t, false
	}
	return rest[close+end+1:], true
}

// cutImageLink removes a leading [![alt](src)](href).
func cutImageLink(t string) (string, bool) {
	rest, ok := strings.CutPrefix(t, "[")
	if !ok {
		return t, false
	}
	rest, ok = cutImage(rest)
	if !ok || !strings.HasPrefix(rest, "](") {
		return t, false
	}
	end := strings.IndexByte(rest, ')')
	if end < 0 {
		return t, false
	}
	return rest[end+1:], true
}

func isSetextUnderline(line string) bool {
	t := strings.TrimSpace(line)
	return len(t) >= 3 && strings.Trim(t, "=") == ""
}
