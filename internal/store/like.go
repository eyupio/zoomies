package store

import "strings"

// likePattern turns the text a person typed into a LIKE argument that matches
// it anywhere in a column and matches only it. LIKE gives % and _ meanings of
// their own, so without this a search for gpu_a100 also found gpu-a100 and a %
// typed into the box found every row. Every LIKE in this package carries
// ESCAPE '\' to go with it.
func likePattern(q string) string {
	return "%" + likeEscape(q) + "%"
}

// likeEscape backslash-escapes the three characters LIKE ... ESCAPE '\' reads
// specially, so the result stands for the literal text.
func likeEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	return strings.ReplaceAll(s, `_`, `\_`)
}
