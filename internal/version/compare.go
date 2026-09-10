package version

import (
	"regexp"
	"strconv"
	"strings"
)

// Skew is how one build relates to another.
type Skew string

const (
	// SkewNone means the two are the same release.
	SkewNone Skew = ""
	// SkewBehind means the first build is an earlier release than the second.
	SkewBehind Skew = "behind"
	// SkewAhead means it is a later one, which for an agent against its
	// controller is the direction nobody tests.
	SkewAhead Skew = "ahead"
	// SkewDiffers means they are not the same and nothing here can say which
	// came first: a development build, a fork's tag, anything not shaped like
	// a release. Saying "differs" is the honest answer, and it is still worth
	// saying -- a fleet running two builds is a fleet whose behaviour has two
	// explanations.
	SkewDiffers Skew = "differs"
)

// CompareBuilds says how build a stands to build b.
//
// It compares releases, not commits. Two builds of one tag are the same
// release: a fleet where they differ has been rebuilt, which is worth knowing
// in a bug report and is not skew, and treating it as skew made every
// development fleet warn about itself constantly.
//
// The parser is deliberately small. It understands the shape this project
// tags -- v0.1-alpha, v1.2.3, 1.2 -- and answers SkewDiffers for anything
// else rather than guessing an order it cannot defend. A wrong order is worse
// than no order: it would tell an operator to upgrade the wrong side.
func CompareBuilds(a, b string) Skew {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	if a == "" || b == "" || a == b {
		return SkewNone
	}
	na, oka := parseRelease(a)
	nb, okb := parseRelease(b)
	if !oka || !okb {
		return SkewDiffers
	}
	for i := range na.parts {
		switch {
		case na.parts[i] < nb.parts[i]:
			return SkewBehind
		case na.parts[i] > nb.parts[i]:
			return SkewAhead
		}
	}
	// Same numbers, different text: a pre-release against the release it leads
	// to. "v1.0-rc1" is behind "v1.0", and an empty pre-release is the later
	// of the two, which is the one rule of pre-release ordering that matters
	// here and the one this project's own tags need.
	switch {
	case na.pre == nb.pre:
		// The same release written two ways -- "v1.2" and "v1.2.0", or with
		// and without the leading v. A person reading the two would say they
		// match, and so does this.
		return SkewNone
	case na.pre == "":
		return SkewAhead
	case nb.pre == "":
		return SkewBehind
	case na.pre < nb.pre:
		return SkewBehind
	default:
		return SkewAhead
	}
}

// release is a tag broken into the three numbers and whatever followed them.
type release struct {
	parts [3]int
	pre   string
}

// parseRelease reads "v1.2.3-rc1" and its shorter forms. Missing numbers are
// zero, so "v1.2" and "v1.2.0" are the same release, which is what a person
// reading the two would say.
func parseRelease(s string) (release, bool) {
	var out release
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if s == "" {
		return out, false
	}
	// The pre-release marker is the first hyphen or plus after the numbers.
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		out.pre, s = s[i+1:], s[:i]
	}
	fields := strings.Split(s, ".")
	if len(fields) == 0 || len(fields) > 3 {
		return out, false
	}
	for i, f := range fields {
		n, err := strconv.Atoi(f)
		if err != nil || n < 0 {
			return out, false
		}
		out.parts[i] = n
	}
	return out, true
}

// releaseLike matches a version stamped from a release tag: 0.2-beta, v1.4.0.
// A build from main is stamped main-sha-abc1234 and matches nothing here.
var releaseLike = regexp.MustCompile(`^v?\d+\.\d+`)

// describeSuffix matches what `git describe` adds to a tag once commits have
// landed on top of it -- v0.2-beta-5-gabc1234 -- which is a local build of
// something past that release rather than the release itself.
var describeSuffix = regexp.MustCompile(`-\d+-g[0-9a-f]{7,}(-dirty)?$`)

// mainBuild matches the version CI stamps into every artifact built from the
// default branch. The moving dev tag carries those artifacts; the commit in
// the stamp is still what makes two concrete builds distinguishable.
var mainBuild = regexp.MustCompile(`^main-sha-[0-9a-f]{7,}$`)

// Release reports the release a build came from, and whether it came from one
// at all.
//
// Two callers need the same answer for different reasons, which is why it lives
// here rather than beside either of them. The update check asks so that it does
// not tell a controller running :dev -- stamped main-sha-abc1234, and usually
// *ahead* of the newest release -- to downgrade. InstallTag builds on the same
// answer to distinguish immutable versioned releases from the moving dev
// channel.
func Release(v string) (string, bool) {
	v = strings.TrimSpace(v)
	if v == "" || !releaseLike.MatchString(v) || describeSuffix.MatchString(v) {
		return "", false
	}
	return strings.TrimPrefix(v, "v"), true
}

// InstallTag returns the downloadable channel that carries this build.
//
// Releases have an immutable v-prefixed tag. Builds from main share the moving
// dev tag, which CI updates for the binary and every stock image on each push.
// A local git-describe build has neither, so callers must not pretend it can be
// downloaded from this repository.
func InstallTag(v string) (string, bool) {
	v = strings.TrimSpace(v)
	if release, ok := Release(v); ok {
		return "v" + release, true
	}
	if v == "dev" || mainBuild.MatchString(v) {
		return "dev", true
	}
	return "", false
}

// Channel is the image/download tag operators should use for this build, or
// empty for a local or otherwise unpublished build.
func Channel(v string) string {
	tag, _ := InstallTag(v)
	return tag
}
