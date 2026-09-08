package version

import (
	"runtime/debug"
	"testing"
)

// A build stamps what it knows. The container image stamps a commit and, until
// it was given a DATE build argument, no date -- and this used to abandon the
// build information the moment a commit was present, so the image reported no
// date at all while the same release installed natively reported one.
func TestEachStampIsFilledInOnItsOwn(t *testing.T) {
	embedded := []debug.BuildSetting{
		{Key: "vcs.revision", Value: "1a2b3c4d5e6f"},
		{Key: "vcs.time", Value: "2026-09-06T08:00:00Z"},
	}
	cases := []struct {
		name                 string
		commit, date         string
		wantCommit, wantDate string
		settings             []debug.BuildSetting
	}{
		{"nothing stamped", "", "", "1a2b3c4d5e6f", "2026-09-06T08:00:00Z", embedded},
		{"a commit stamped, no date", "deadbeef", "", "deadbeef", "2026-09-06T08:00:00Z", embedded},
		{"both stamped", "deadbeef", "yesterday", "deadbeef", "yesterday", embedded},
		{"nothing anywhere", "", "", "", "", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			commit, date := fill(tc.commit, tc.date, tc.settings)
			if commit != tc.wantCommit || date != tc.wantDate {
				t.Fatalf("fill = %q, %q; want %q, %q", commit, date, tc.wantCommit, tc.wantDate)
			}
		})
	}
}
