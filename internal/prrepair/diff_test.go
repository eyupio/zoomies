package prrepair

import "testing"

func TestTheSuppliedDiffShowsTheHeadSideOfEachHunk(t *testing.T) {
	const diff = "a.txt\n@@ -1,3 +1,3 @@\n context\n-old\n+new\n tail\nb.txt\n@@ -4,2 +4,2 @@\n before\n+added\n"
	got := diffSource(diff)
	if len(got["a.txt"]) != 1 || got["a.txt"][0] != "context\nnew\ntail\n" {
		t.Fatalf("a.txt: %q", got["a.txt"])
	}
	if len(got["b.txt"]) != 1 || got["b.txt"][0] != "before\nadded\n" {
		t.Fatalf("b.txt: %q", got["b.txt"])
	}
}

// A hunk says the last line has no newline. The head-side text has to end there
// too, or a preimage ending at that line would not match the file it edits.
func TestANoNewlineMarkerTakesTheNewlineOffTheLineBeforeIt(t *testing.T) {
	const diff = "a.txt\n@@ -1 +1 @@\n-old\n+new\n\\ No newline at end of file\n"
	got := diffSource(diff)["a.txt"]
	if len(got) != 1 || got[0] != "new" {
		t.Fatalf("%q", got)
	}
}

// Hunks are separate pieces of the file, so text quoted from one cannot join to
// a line in the next. Treating them as one text would let an edit name text the
// model never saw together.
func TestAPreimageCannotJoinTwoHunks(t *testing.T) {
	s := newSourceContext(nil, false)
	s.inDiff = diffSource("a.txt\n@@ -1 +1 @@\n+one\n@@ -10 +10 @@\n+two\n")
	if !s.shownInDiff("a.txt", "one\n") || !s.shownInDiff("a.txt", "two\n") {
		t.Fatal("a hunk's own text was not supplied")
	}
	if s.shownInDiff("a.txt", "one\ntwo\n") {
		t.Fatal("text joined across hunks was treated as supplied")
	}
}
