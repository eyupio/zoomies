package store

import (
	"strings"
	"testing"
)

// LooksGenerated is what tells a seeded fixture from a real row, so it has to
// agree with NewID exactly: a generated identifier always passes, and the
// readable identifiers the demo seed writes never do.
func TestLooksGeneratedAgreesWithNewID(t *testing.T) {
	for range 200 {
		id := NewID(PrefixPool)
		if !LooksGenerated(id) {
			t.Fatalf("NewID produced %q, which LooksGenerated does not recognise", id)
		}
	}
	for _, id := range []string{
		"pool_demolinux", "ins_demoacme", "host_demoa", "run_demo00", "job_demo051",
		"usr_demo_alice", "pool_", "nounderscore", "",
		// Right length, wrong alphabet: base32 here has no 0, 1, 8 or 9 and no
		// upper case.
		"pool_abcdefghijk01", "pool_ABCDEFGHIJKLM",
		// Right alphabet, wrong length.
		"pool_abcdefghijkl", "pool_abcdefghijklmn",
	} {
		if LooksGenerated(id) {
			t.Errorf("LooksGenerated(%q) = true, want false", id)
		}
	}
}

// The length constant is a property of NewID's byte count, and a change to
// one without the other would silently make every real identifier look like a
// fixture, or every fixture look real.
func TestGeneratedIDLengthMatchesNewID(t *testing.T) {
	_, rest, _ := strings.Cut(NewID(PrefixJob), "_")
	if len(rest) != generatedIDLength {
		t.Fatalf("NewID's random part is %d characters, generatedIDLength says %d", len(rest), generatedIDLength)
	}
}
