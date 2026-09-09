package store

import "strings"

// Runner labels somebody else operates.
//
// A job whose labels all name GitHub's own runners, or a hosted-runner
// vendor's, runs where its labels say. This fleet has no pool for it and never
// will, so it is neither this fleet's work nor this fleet's fault, and every
// question the product answers about "our jobs" has to be able to leave it out
// -- the Jobs page's default view, the Overview's counts, the usage figures and
// the unmatched warning alike.
//
// The lists live here rather than in internal/migrate, which is where the
// migration wizard first needed them, because internal/store is what the SQL
// asking the same question is written against and internal/migrate imports it.
// migrate.IsHostedLabel and migrate.IsManagedLabel are now the wizard's names
// for these.

// hostedPrefixes are the runner labels GitHub itself provides, as of the runner
// images published for github.com.
//
// It is a prefix list, not an exact one: GitHub keeps adding sizes and versions
// ("ubuntu-22.04-arm", "windows-11-arm", the larger-runner names an
// organisation configures), and a list of only the exact names we shipped with
// would go quietly blind as they change.
var hostedPrefixes = []string{"ubuntu-", "windows-", "macos-"}

// vendorPrefixes are the label shapes of the hosted-runner vendors that sit in
// front of GitHub Actions -- Blacksmith, BuildJet, WarpBuild, Namespace, Depot
// and Ubicloud.
//
// They belong beside GitHub's own for the same reason: a repository on
// "blacksmith-4vcpu-ubuntu-2404" is renting somebody else's machines by the
// minute, and this fleet has no more to do with that job than with one on
// "ubuntu-latest". A prefix list again, and for the same reason -- every one of
// these vendors keeps adding sizes.
var vendorPrefixes = []string{
	"blacksmith",
	"buildjet-",
	"warp-",
	"namespace-profile-",
	"nscloud-",
	"depot-",
	"ubicloud",
}

// IsHostedLabel reports whether label is one of GitHub's own runner labels.
func IsHostedLabel(label string) bool {
	return hasAnyPrefix(NormalizeLabel(label), hostedPrefixes)
}

// IsManagedLabel reports whether label names a runner somebody else operates:
// GitHub's own, or one of the vendors above. The distinction that matters to an
// operator is not "GitHub or not" but "rented or ours".
func IsManagedLabel(label string) bool {
	l := NormalizeLabel(label)
	return hasAnyPrefix(l, hostedPrefixes) || hasAnyPrefix(l, vendorPrefixes)
}

func hasAnyPrefix(label string, prefixes []string) bool {
	if label == "" {
		return false
	}
	for _, p := range prefixes {
		if strings.HasPrefix(label, p) {
			return true
		}
	}
	return false
}

// HostedJob reports whether every label a job asked for names a runner somebody
// else operates. Such a job is theirs to run: it being unmatched here is what
// the workflow asked for rather than a job going nowhere.
//
// A job with no labels at all is not hosted. GitHub always sends the labels it
// resolved, so an empty set means a delivery we could not read rather than a
// job nobody has to run, and guessing "somebody else's" about it would hide it
// from the one page that could show the gap.
func HostedJob(labels []string) bool {
	if len(labels) == 0 {
		return false
	}
	for _, l := range labels {
		if !IsManagedLabel(l) {
			return false
		}
	}
	return true
}

// hostedJobSQL is HostedJob in SQL, over the JSON array the labels are stored
// as. The two spellings are built from the same prefix lists, because a page
// that hides a vendor's job while the warning above it still counts one is
// worse than either behaviour on its own.
//
// The caller names the jobs table as its own query spells it, because pools
// have labels too and a query that joins the two cannot say `labels` at all.
func hostedJobSQL(jobs string) string {
	tests := make([]string, 0, len(hostedPrefixes)+len(vendorPrefixes))
	for _, p := range append(append([]string{}, hostedPrefixes...), vendorPrefixes...) {
		// The prefixes are literal words and hyphens, but ESCAPE keeps that
		// from being load-bearing: a vendor prefix with an underscore in it
		// would otherwise match anything.
		tests = append(tests, `value NOT LIKE '`+likeEscape(p)+`%' ESCAPE '\'`)
	}
	return `(json_array_length(` + jobs + `.labels) > 0 AND NOT EXISTS (
		SELECT 1 FROM json_each(` + jobs + `.labels) WHERE ` + strings.Join(tests, " AND ") + `))`
}
