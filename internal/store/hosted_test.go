package store

import "testing"

// The prefixes are what tells a runner this fleet could own from one it never
// will, and both halves of the product read them: the migration wizard offers
// to migrate the labels, and the Jobs page decides from them whether a job is
// this fleet's business at all.
func TestManagedLabelsAreTheOnesSomebodyElseOperates(t *testing.T) {
	for _, tc := range []struct {
		label   string
		hosted  bool
		managed bool
	}{
		{"ubuntu-latest", true, true},
		{"ubuntu-22.04-arm", true, true},
		{"windows-11-arm", true, true},
		// GitHub's own images are written both ways in the wild, and a label
		// is case-insensitive to GitHub, so it must be here too.
		{"macOS-14", true, true},
		{"blacksmith-4vcpu-ubuntu-2404", false, true},
		{"buildjet-8vcpu-ubuntu-2204", false, true},
		{"ubicloud-standard-2", false, true},
		// Ours, or an organisation's own invention. Neither is somebody
		// else's to run, and a job asking for one waits on this fleet.
		{"self-hosted", false, false},
		{"gpu", false, false},
		{"ubuntu", false, false},
		{"", false, false},
	} {
		if got := IsHostedLabel(tc.label); got != tc.hosted {
			t.Errorf("IsHostedLabel(%q) = %v, want %v", tc.label, got, tc.hosted)
		}
		if got := IsManagedLabel(tc.label); got != tc.managed {
			t.Errorf("IsManagedLabel(%q) = %v, want %v", tc.label, got, tc.managed)
		}
	}
}

// A job is somebody else's only when every label says so. One label this fleet
// could answer makes the job ours to run, or ours to explain.
func TestOneLabelThisFleetCouldAnswerMakesTheJobOurs(t *testing.T) {
	for _, tc := range []struct {
		name   string
		labels []string
		want   bool
	}{
		{"a vendor's", []string{"blacksmith-4vcpu-ubuntu-2404"}, true},
		{"GitHub's own", []string{"ubuntu-latest"}, true},
		{"mixed", []string{"ubuntu-latest", "gpu"}, false},
		{"ours", []string{"self-hosted", "linux", "x64"}, false},
		// A delivery we could not read the labels of, rather than a job
		// nobody has to run: guessing "somebody else's" would hide it.
		{"none at all", nil, false},
	} {
		if got := HostedJob(tc.labels); got != tc.want {
			t.Errorf("HostedJob(%v) [%s] = %v, want %v", tc.labels, tc.name, got, tc.want)
		}
	}
}
