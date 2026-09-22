package controller

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var codeLiteral = regexp.MustCompile(`Code: *"([a-z_]+\.[a-z_.]+)"`)

// raisedCodes is every problem code this package and the providers construct,
// found by walking rather than listed, for the reason internal/docs walks the
// same trees: a hand-written list catches up to a new file only after somebody
// notices, and by then the code has shipped to whichever audience the default
// chose.
func raisedCodes(t *testing.T) []string {
	t.Helper()
	seen := map[string]bool{}
	for _, root := range []string{".", "../provider"} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, m := range codeLiteral.FindAllStringSubmatch(string(body), -1) {
				seen[m[1]] = true
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", root, err)
		}
	}
	out := make([]string, 0, len(seen))
	for c := range seen {
		out = append(out, c)
	}
	return out
}

// Whose a problem is has to be decided by somebody, once, for every code. The
// default is the platform, so a code nobody classified goes to the smaller
// audience rather than leaking -- but a gap in the fleet's list is still a
// bug, and this is what stops one reaching a release.
func TestEveryProblemCodeHasAnAudience(t *testing.T) {
	var missing []string
	for _, code := range raisedCodes(t) {
		if _, ok := problemAudience[code]; !ok {
			missing = append(missing, code)
		}
	}
	if len(missing) > 0 {
		t.Errorf("these problem codes are raised with no audience decided for them, so they default to the platform and the fleet never sees them:\n  %s",
			strings.Join(missing, "\n  "))
	}
}

// And the other way: a row for a code nothing raises any more is a decision
// about nothing, and the next person to read the table has to work out
// whether the code went away or the raise did.
func TestTheAudienceTableNamesNothingThatIsGone(t *testing.T) {
	raised := map[string]bool{}
	for _, c := range raisedCodes(t) {
		raised[c] = true
	}
	var stale []string
	for code := range problemAudience {
		if !raised[code] {
			stale = append(stale, code)
		}
	}
	if len(stale) > 0 {
		t.Errorf("the audience table names codes nothing raises:\n  %s", strings.Join(stale, "\n  "))
	}
}

// The split is a judgement, and these are the ones worth pinning: each is a
// case where getting it wrong is a leak or a silence rather than a matter of
// taste.
func TestTheAudienceOfTheOnesThatMatter(t *testing.T) {
	for _, tc := range []struct {
		code string
		want Audience
		why  string
	}{
		{"backup.remote_unreadable", AudiencePlatform, "a backup remote is the process's, and its existence is not the fleet's to learn"},
		{"backup.failed", AudiencePlatform, "the fleet cannot take a backup and cannot fix one failing"},
		{"controller.lease_lost", AudiencePlatform, "the lease is between the process and its database"},
		{"controller.loop_panicked", AudiencePlatform, "a panic in a loop is the operator's to read the log for"},
		{"controller.update_available", AudiencePlatform, "only whoever runs the process can upgrade it"},
		{"capacity_demand.delivery_failed", AudiencePlatform, "the receiver and its signing secret are the platform's"},
		{"crypto.key_mismatch", AudiencePlatform, "the encryption key is the process's"},
		{"controller.problems_partial", AudienceBoth, "a truncated list must say so to whoever is reading it"},
		{"pool.no_capacity", AudienceFleet, "a pool with nowhere to run is the fleet's to resize or re-label"},
		{"host.unhealthy", AudienceFleet, "the fleet's own hosts"},
		{"runners.failed", AudienceFleet, "the fleet's own runners"},
		{"jobs.unmatched", AudienceFleet, "a job no pool claims is a labelling question for whoever wrote the workflow"},
		{"recovery.fenced", AudienceFleet, "the fence stops the fleet's work, so the fleet has to be told it is why nothing is running"},
	} {
		if got := audienceFor(tc.code); got != tc.want {
			t.Errorf("%s is %q, want %q: %s", tc.code, got, tc.want, tc.why)
		}
	}
}

// For is what every filter below is written in terms of, so its own edges are
// worth holding: the platform sees an unclassified problem, the fleet does
// not, and both see the ones marked both.
func TestWhoSeesWhat(t *testing.T) {
	for _, tc := range []struct {
		a                     Audience
		platform, fleetMember bool
	}{
		{AudiencePlatform, true, false},
		{AudienceFleet, false, true},
		{AudienceBoth, true, true},
		{Audience(""), true, false},
	} {
		if got := tc.a.For(true); got != tc.platform {
			t.Errorf("%q for the platform = %v, want %v", tc.a, got, tc.platform)
		}
		if got := tc.a.For(false); got != tc.fleetMember {
			t.Errorf("%q for the fleet = %v, want %v", tc.a, got, tc.fleetMember)
		}
	}
}
