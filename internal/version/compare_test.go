package version

import "testing"

// The comparison exists to tell an operator which side to upgrade, so the
// answer it must never get wrong is the direction. A wrong order sends them to
// the wrong machine; "differs" sends them to look, which is always safe.
func TestCompareBuildsOrdersReleasesAndRefusesToGuess(t *testing.T) {
	cases := []struct {
		a, b string
		want Skew
	}{
		// The same release, however it is written.
		{"v1.2.3", "v1.2.3", SkewNone},
		{"v1.2", "v1.2.0", SkewNone},
		{"1.2.3", "v1.2.3", SkewNone},
		{"", "v1.2.3", SkewNone},
		{"v1.2.3", "", SkewNone},

		// Ordered.
		{"v1.2.3", "v1.2.4", SkewBehind},
		{"v1.2.4", "v1.2.3", SkewAhead},
		{"v1.2.3", "v1.3.0", SkewBehind},
		{"v2.0.0", "v1.9.9", SkewAhead},
		{"v0.1-alpha", "v0.2", SkewBehind},

		// A pre-release leads to the release it names.
		{"v1.0-rc1", "v1.0", SkewBehind},
		{"v1.0", "v1.0-rc1", SkewAhead},
		{"v1.0-rc1", "v1.0-rc2", SkewBehind},

		// Two builds of one tag are the same release. Treating a rebuild as
		// skew made every development fleet warn about itself.
		{"dev", "dev", SkewNone},

		// Nothing here can order these, and guessing would be worse than
		// saying so.
		{"dev", "v1.2.3", SkewDiffers},
		{"v1.2.3", "some-fork-build", SkewDiffers},
		{"v1.2.3.4", "v1.2.3", SkewDiffers},
		{"v1.x", "v1.2", SkewDiffers},
	}

	for _, tc := range cases {
		if got := CompareBuilds(tc.a, tc.b); got != tc.want {
			t.Errorf("CompareBuilds(%q, %q) = %q, want %q", tc.a, tc.b, got, tc.want)
		}
	}
}

// Whatever the pair, reversing it reverses the answer. It is the property a
// direction has to have, and the one a hand-written comparator loses first.
func TestCompareBuildsIsSymmetric(t *testing.T) {
	opposite := map[Skew]Skew{
		SkewNone: SkewNone, SkewDiffers: SkewDiffers,
		SkewBehind: SkewAhead, SkewAhead: SkewBehind,
	}
	builds := []string{"", "dev", "v0.1-alpha", "v0.1", "v0.2", "v1.0-rc1", "v1.0", "v1.2.3", "weird"}
	for _, a := range builds {
		for _, b := range builds {
			forward, back := CompareBuilds(a, b), CompareBuilds(b, a)
			if back != opposite[forward] {
				t.Errorf("CompareBuilds(%q,%q) = %q but CompareBuilds(%q,%q) = %q", a, b, forward, b, a, back)
			}
		}
	}
}

func TestInstallTagNamesThePublishedChannel(t *testing.T) {
	for _, tc := range []struct {
		build string
		tag   string
		ok    bool
	}{
		{"1.2.3", "v1.2.3", true},
		{"v1.2.3", "v1.2.3", true},
		{"0.2-beta", "v0.2-beta", true},
		{"main-sha-117bc18", "dev", true},
		{"dev", "dev", true},
		{"v0.2-beta-276-g394254b", "", false},
		{"some-fork-build", "", false},
	} {
		t.Run(tc.build, func(t *testing.T) {
			got, ok := InstallTag(tc.build)
			if got != tc.tag || ok != tc.ok {
				t.Errorf("InstallTag(%q) = %q, %v; want %q, %v", tc.build, got, ok, tc.tag, tc.ok)
			}
			if channel := Channel(tc.build); channel != tc.tag {
				t.Errorf("Channel(%q) = %q, want %q", tc.build, channel, tc.tag)
			}
		})
	}
}
