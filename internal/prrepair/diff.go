package prrepair

import "strings"

// diffSource is the head-side text of each hunk in the PR's bounded diff, by
// path. The diff is part of what a repair sends the model, and a large file
// arrives as a window that often stops short of the hunk the failing change is
// in. A targeted edit built from those lines quotes text the model was shown, so
// the edit check has to know it; otherwise a fix the model could read off the
// diff is refused as unseen source.
//
// A hunk's head-side text is its context and added lines in order, with the
// one-column diff marker removed. Deleted lines are left out because they are
// not in the file the edit applies to, and a "\ No newline at end of file"
// marker takes the newline off the line before it.
func diffSource(diff string) map[string][]string {
	out := map[string][]string{}
	path := ""
	head := []string{}
	flush := func() {
		if path != "" && len(head) > 0 {
			out[path] = append(out[path], strings.Join(head, ""))
		}
		head = head[:0]
	}
	for _, line := range strings.SplitAfter(diff, "\n") {
		if line == "" {
			continue
		}
		switch line[0] {
		case '@':
			flush()
		case '+', ' ':
			head = append(head, line[1:])
		case '-':
			// Deleted source is not in the head the edit applies to.
		case '\\':
			if n := len(head); n > 0 {
				head[n-1] = strings.TrimSuffix(head[n-1], "\n")
			}
		default:
			if name := strings.TrimSuffix(line, "\n"); name != "" {
				flush()
				path = name
			}
		}
	}
	flush()
	return out
}
