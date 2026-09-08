package migrate

import (
	"slices"
	"strings"
	"testing"
)

// zoomies is the mapping a fleet with one Linux pool would produce.
var zoomies = Mapping{Labels: map[string]string{
	"ubuntu-latest": "zoomies-linux-x64",
	"ubuntu-22.04":  "zoomies-linux-x64",
	"ubuntu-24.04":  "zoomies-linux-x64",
}}

func TestFileRewritesTheCommonForms(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
		job  string
	}{
		{
			name: "a scalar value",
			in:   "jobs:\n  build:\n    runs-on: ubuntu-latest\n",
			want: "jobs:\n  build:\n    runs-on: zoomies-linux-x64\n",
			job:  "build",
		},
		{
			name: "a single-item flow sequence",
			in:   "jobs:\n  test:\n    runs-on: [ubuntu-latest]\n",
			want: "jobs:\n  test:\n    runs-on: zoomies-linux-x64\n",
			job:  "test",
		},
		{
			name: "a quoted value",
			in:   "jobs:\n  test:\n    runs-on: \"ubuntu-22.04\"\n",
			want: "jobs:\n  test:\n    runs-on: zoomies-linux-x64\n",
			job:  "test",
		},
		{
			name: "a block sequence collapses",
			in:   "jobs:\n  lint:\n    runs-on:\n      - ubuntu-latest\n    steps: []\n",
			want: "jobs:\n  lint:\n    runs-on: zoomies-linux-x64\n    steps: []\n",
			job:  "lint",
		},
		{
			name: "a trailing comment survives",
			in:   "jobs:\n  build:\n    runs-on: ubuntu-latest # the cheap one\n",
			want: "jobs:\n  build:\n    runs-on: zoomies-linux-x64 # the cheap one\n",
			job:  "build",
		},
		{
			name: "an unusual key case",
			in:   "jobs:\n  build:\n    Runs-On: ubuntu-latest\n",
			want: "jobs:\n  build:\n    Runs-On: zoomies-linux-x64\n",
			job:  "build",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := File(c.in, zoomies)
			if got.Content != c.want {
				t.Fatalf("content =\n%q\nwant\n%q", got.Content, c.want)
			}
			if len(got.Rewrites) != 1 {
				t.Fatalf("rewrites = %+v, want exactly one", got.Rewrites)
			}
			if got.Rewrites[0].Job != c.job {
				t.Errorf("job = %q, want %q", got.Rewrites[0].Job, c.job)
			}
			if got.Rewrites[0].To != "zoomies-linux-x64" {
				t.Errorf("to = %q, want the mapped label", got.Rewrites[0].To)
			}
			if len(got.Skips) != 0 {
				t.Errorf("skips = %+v, want none", got.Skips)
			}
		})
	}
}

func TestFileLeavesEverythingElseByteForByte(t *testing.T) {
	in := `# Continuous integration.
#
# Kept deliberately boring.
name: CI

on:
  push:
    branches: [main]        # only main
  pull_request:

env:
  CGO_ENABLED: "0"

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Build
        run: |
          make build
          make test
  release:
    needs: [build]
    if: startsWith(github.ref, 'refs/tags/')
    runs-on: ubuntu-22.04
    steps:
      - run: echo "runs-on: not-a-key"
`
	got := File(in, zoomies)
	if len(got.Rewrites) != 2 {
		t.Fatalf("rewrites = %+v, want two", got.Rewrites)
	}
	// Only the two runs-on lines may differ.
	before, after := strings.Split(in, "\n"), strings.Split(got.Content, "\n")
	if len(before) != len(after) {
		t.Fatalf("the rewrite changed the line count: %d -> %d", len(before), len(after))
	}
	changed := 0
	for i := range before {
		if before[i] != after[i] {
			changed++
			if !strings.Contains(after[i], "zoomies-linux-x64") {
				t.Errorf("line %d changed to something unexpected: %q", i+1, after[i])
			}
		}
	}
	if changed != 2 {
		t.Fatalf("%d lines changed, want exactly the two runs-on lines", changed)
	}
	// A step that merely mentions runs-on in a string is not a runs-on.
	if !strings.Contains(got.Content, `- run: echo "runs-on: not-a-key"`) {
		t.Error("a run: line that mentions runs-on was rewritten")
	}
}

func TestFileSkipsWhatItCannotBeSureOf(t *testing.T) {
	cases := []struct {
		name       string
		in         string
		wantReason string
	}{
		{
			name:       "a matrix expression",
			in:         "jobs:\n  build:\n    runs-on: ${{ matrix.os }}\n",
			wantReason: "expression",
		},
		{
			name:       "already self-hosted",
			in:         "jobs:\n  build:\n    runs-on: [self-hosted, linux, x64]\n",
			wantReason: "already runs on a self-hosted runner",
		},
		{
			name:       "a label the organisation invented",
			in:         "jobs:\n  build:\n    runs-on: acme-bigbox\n",
			wantReason: "already pointed somewhere deliberate",
		},
		{
			name:       "a hosted label nobody mapped",
			in:         "jobs:\n  build:\n    runs-on: windows-latest\n",
			wantReason: `"windows-latest" is not mapped to a pool`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := File(c.in, zoomies)
			if got.Content != c.in {
				t.Fatalf("a skipped file was modified:\n%q", got.Content)
			}
			if got.Changed() {
				t.Fatal("Changed() is true for a file nothing was rewritten in")
			}
			if len(got.Skips) != 1 {
				t.Fatalf("skips = %+v, want exactly one", got.Skips)
			}
			if !strings.Contains(got.Skips[0].Reason, c.wantReason) {
				t.Errorf("reason = %q, want it to mention %q", got.Skips[0].Reason, c.wantReason)
			}
			if got.Skips[0].Job != "build" {
				t.Errorf("job = %q, want build", got.Skips[0].Job)
			}
		})
	}
}

// A file with no mapping at all must be left completely alone: this is the
// state the wizard is in before the operator has chosen anything.
func TestFileWithAnEmptyMappingChangesNothing(t *testing.T) {
	in := "jobs:\n  build:\n    runs-on: ubuntu-latest\n"
	got := File(in, Mapping{})
	if got.Content != in || got.Changed() {
		t.Fatalf("an empty mapping rewrote the file:\n%q", got.Content)
	}
	if len(got.Skips) != 1 || !strings.Contains(got.Skips[0].Reason, "not mapped") {
		t.Fatalf("skips = %+v, want one that says the label is unmapped", got.Skips)
	}
}

func TestFileKeepsCRLFAndAMissingFinalNewline(t *testing.T) {
	in := "jobs:\r\n  build:\r\n    runs-on: ubuntu-latest"
	got := File(in, zoomies)
	want := "jobs:\r\n  build:\r\n    runs-on: zoomies-linux-x64"
	if got.Content != want {
		t.Fatalf("content = %q, want %q", got.Content, want)
	}
}

func TestFileRewritesEveryJob(t *testing.T) {
	in := `jobs:
  build:
    runs-on: ubuntu-latest
  test:
    runs-on: ubuntu-24.04
  windows:
    runs-on: windows-latest
`
	got := File(in, zoomies)
	if len(got.Rewrites) != 2 {
		t.Fatalf("rewrites = %+v, want the two Ubuntu jobs", got.Rewrites)
	}
	jobs := []string{got.Rewrites[0].Job, got.Rewrites[1].Job}
	if jobs[0] != "build" || jobs[1] != "test" {
		t.Errorf("jobs = %v, want [build test]", jobs)
	}
	if len(got.Skips) != 1 || got.Skips[0].Job != "windows" {
		t.Errorf("skips = %+v, want the unmapped windows job", got.Skips)
	}
}

// A `runs-on` inside a composite action or a reusable workflow sits at a
// different depth. The job name may be wrong there, but the rewrite must not
// be.
func TestFileRewritesOutsideAJobsBlock(t *testing.T) {
	in := "on:\n  workflow_call:\njobs:\n  call:\n    uses: ./.github/workflows/inner.yml\n"
	got := File(in, zoomies)
	if got.Changed() {
		t.Fatalf("nothing to rewrite, but got %+v", got.Rewrites)
	}
}

func TestHostedLabelsIn(t *testing.T) {
	in := `jobs:
  a:
    runs-on: ubuntu-latest
  b:
    runs-on: [macos-14]
  c:
    runs-on:
      - windows-latest
  d:
    runs-on: ubuntu-latest
  e:
    runs-on: [self-hosted, linux]
  f:
    runs-on: ${{ matrix.os }}
`
	got := HostedLabelsIn(in)
	want := []string{"ubuntu-latest", "macos-14", "windows-latest"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("HostedLabelsIn = %v, want %v", got, want)
	}
}

func TestIsHostedLabel(t *testing.T) {
	hosted := []string{"ubuntu-latest", "ubuntu-22.04", "ubuntu-24.04-arm", "windows-2022", "macos-14", "macOS-latest", "ubuntu-latest-4-cores"}
	for _, l := range hosted {
		if !IsHostedLabel(l) {
			t.Errorf("IsHostedLabel(%q) = false, want true", l)
		}
	}
	notHosted := []string{"", "self-hosted", "linux", "x64", "zoomies-linux-x64", "gpu", "buildjet-4vcpu-ubuntu-2204"}
	for _, l := range notHosted {
		if IsHostedLabel(l) {
			t.Errorf("IsHostedLabel(%q) = true, want false", l)
		}
	}
}

func TestSplitFlowItemsKeepsQuotedCommas(t *testing.T) {
	got := splitFlowItems(`ubuntu-latest, "a,b", 'c'`)
	want := []string{"ubuntu-latest", `"a,b"`, `'c'`}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("splitFlowItems = %v, want %v", got, want)
	}
}

func TestEmptyFile(t *testing.T) {
	got := File("", zoomies)
	if got.Content != "" || got.Changed() {
		t.Fatalf("an empty file produced %+v", got)
	}
}

// A repository on a hosted-runner vendor is the one this fleet most wants to
// take over, so its labels have to be migratable. Before this, every one of
// them was read as "already pointed somewhere deliberate" and an operator was
// shown a wizard with nothing in it and no reason why.
func TestFileRewritesVendorHostedLabels(t *testing.T) {
	m := Mapping{Labels: map[string]string{
		"blacksmith-4vcpu-ubuntu-2404":  "zoomies-linux-x64",
		"warp-ubuntu-latest-x64-4x":     "zoomies-linux-x64",
		"nscloud-ubuntu-22.04-amd64-4x": "zoomies-linux-x64",
	}}
	for _, label := range []string{"blacksmith-4vcpu-ubuntu-2404", "warp-ubuntu-latest-x64-4x", "nscloud-ubuntu-22.04-amd64-4x"} {
		t.Run(label, func(t *testing.T) {
			got := File("jobs:\n  build:\n    runs-on: "+label+"\n", m)
			if !got.Changed() {
				t.Fatalf("%q was not rewritten; skips = %+v", label, got.Skips)
			}
			if !strings.Contains(got.Content, "runs-on: zoomies-linux-x64") {
				t.Fatalf("content = %q", got.Content)
			}
		})
	}
}

// The mapping step is built from this, so a vendor label has to reach it or an
// operator has nothing to map.
func TestHostedLabelsInFindsVendorLabels(t *testing.T) {
	in := "jobs:\n  a:\n    runs-on: blacksmith-4vcpu-ubuntu-2404\n  b:\n    runs-on: ubuntu-latest\n  c:\n    runs-on: acme-bigbox\n"
	got := HostedLabelsIn(in)
	want := []string{"blacksmith-4vcpu-ubuntu-2404", "ubuntu-latest"}
	if len(got) != len(want) {
		t.Fatalf("HostedLabelsIn = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("HostedLabelsIn = %v, want %v", got, want)
		}
	}
}

func TestIsManagedLabelSeparatesRentedFromOurs(t *testing.T) {
	rented := []string{"ubuntu-latest", "macOS-13", "blacksmith", "blacksmith-8vcpu-ubuntu-2404",
		"buildjet-4vcpu-ubuntu-2204", "warp-ubuntu-latest-x64-2x", "namespace-profile-default",
		"nscloud-ubuntu-22.04-amd64-4x", "depot-ubuntu-24.04", "ubicloud-standard-4"}
	for _, l := range rented {
		if !IsManagedLabel(l) {
			t.Errorf("IsManagedLabel(%q) = false, want true", l)
		}
	}
	ours := []string{"self-hosted", "zoomies-linux-x64", "acme-bigbox", "linux", "x64", ""}
	for _, l := range ours {
		if IsManagedLabel(l) {
			t.Errorf("IsManagedLabel(%q) = true, want false", l)
		}
	}
}

// A block-sequence item can carry a comment of its own. The label offered to
// the wizard is the label, not "ubuntu-latest # pinned"; and since the mapping
// replaces the item the comment was about, the rewrite leaves such a job alone
// and says why rather than collapsing the list over the comment. A comment or
// blank line between two items is the same case, and used to be worse: the
// items above it were collapsed and the ones below left dangling.
func TestCommentsInsideARunsOnListAreKeptByLeavingTheJobAlone(t *testing.T) {
	withItemComment := "jobs:\n  build:\n    runs-on:\n      - ubuntu-latest # pinned\n    steps: []\n"
	withCommentLine := "jobs:\n  build:\n    runs-on:\n      - ubuntu-latest\n      # and nothing else\n      - foo\n    steps: []\n"
	withBlankLine := "jobs:\n  build:\n    runs-on:\n      - ubuntu-latest\n\n      - foo\n    steps: []\n"

	if got := HostedLabelsIn(withItemComment); !slices.Equal(got, []string{"ubuntu-latest"}) {
		t.Fatalf("HostedLabelsIn = %q, want the label without its comment", got)
	}
	if got := HostedLabelsIn(withCommentLine); !slices.Equal(got, []string{"ubuntu-latest"}) {
		t.Fatalf("HostedLabelsIn over a comment line = %q", got)
	}

	for name, in := range map[string]string{"item comment": withItemComment, "comment line": withCommentLine, "blank line": withBlankLine} {
		t.Run(name, func(t *testing.T) {
			got := File(in, zoomies)
			if got.Content != in {
				t.Fatalf("content changed:\n%q\nwant it left alone\n%q", got.Content, in)
			}
			if len(got.Rewrites) != 0 || len(got.Skips) != 1 {
				t.Fatalf("rewrites = %+v, skips = %+v; want one skip and no rewrite", got.Rewrites, got.Skips)
			}
			if !strings.Contains(got.Skips[0].Reason, "collapsing the list would delete") || got.Skips[0].Job != "build" {
				t.Fatalf("skip = %+v, want it to name the comment and the job", got.Skips[0])
			}
		})
	}

	// A comment line after the list belongs to what follows, and the list is
	// still rewritten.
	after := "jobs:\n  build:\n    runs-on:\n      - ubuntu-latest\n    # steps follow\n    steps: []\n"
	got := File(after, zoomies)
	if want := "jobs:\n  build:\n    runs-on: zoomies-linux-x64\n    # steps follow\n    steps: []\n"; got.Content != want {
		t.Fatalf("content =\n%q\nwant\n%q", got.Content, want)
	}
	if label, comment := splitItemComment(`"a # b" # c`); label != `"a # b"` || comment != "# c" {
		t.Fatalf("splitItemComment = %q, %q; a # inside quotes is part of the label", label, comment)
	}
}

// ---------------------------------------------------------------------------
// Overrides
// ---------------------------------------------------------------------------

// The consolidated mapping is the default, and an override is the exception to
// it. A fleet answers "where does ubuntu-latest go" once; a job that needs
// somewhere else says so by name.
func TestOverrideBeatsTheConsolidatedMappingForOneJob(t *testing.T) {
	in := "jobs:\n  build:\n    runs-on: ubuntu-latest\n  integration:\n    runs-on: ubuntu-latest\n"
	m := zoomies
	m.Overrides = []Override{{
		Repo: "acme/widgets", Path: ".github/workflows/ci.yml", Job: "integration", To: "zoomies-big",
	}}

	got := File(in, m.In("acme/widgets", ".github/workflows/ci.yml"))
	if len(got.Rewrites) != 2 {
		t.Fatalf("rewrites = %+v, want both jobs", got.Rewrites)
	}
	if got.Rewrites[0].To != "zoomies-linux-x64" || got.Rewrites[0].Overridden {
		t.Errorf("build = %+v, want the consolidated mapping", got.Rewrites[0])
	}
	if got.Rewrites[1].To != "zoomies-big" || !got.Rewrites[1].Overridden {
		t.Errorf("integration = %+v, want the override, marked as one", got.Rewrites[1])
	}
	if !strings.Contains(got.Content, "    runs-on: zoomies-big\n") {
		t.Errorf("the override did not reach the file:\n%s", got.Content)
	}
}

// An override belongs to one place. The same job name in another repository or
// another file is a different job, and quietly migrating it would be a change
// nobody reviewed.
func TestOverrideNamesExactlyOnePlace(t *testing.T) {
	in := "jobs:\n  build:\n    runs-on: ubuntu-latest\n"
	m := zoomies
	m.Overrides = []Override{{
		Repo: "acme/widgets", Path: ".github/workflows/ci.yml", Job: "build", To: "zoomies-big",
	}}

	elsewhere := []struct{ name, repo, path string }{
		{"another repository", "acme/other", ".github/workflows/ci.yml"},
		{"another file", "acme/widgets", ".github/workflows/release.yml"},
	}
	for _, c := range elsewhere {
		t.Run(c.name, func(t *testing.T) {
			got := File(in, m.In(c.repo, c.path))
			if len(got.Rewrites) != 1 || got.Rewrites[0].To != "zoomies-linux-x64" {
				t.Fatalf("rewrites = %+v, want the consolidated mapping untouched", got.Rewrites)
			}
			if got.Rewrites[0].Overridden {
				t.Error("a job in another place was reported as overridden")
			}
		})
	}
	// Repository names are case-insensitive on GitHub, and an operator who
	// typed "ACME/Widgets" meant the same repository.
	if got := File(in, m.In("ACME/Widgets", ".github/workflows/ci.yml")); got.Rewrites[0].To != "zoomies-big" {
		t.Errorf("to = %q, want the override to survive a difference in case", got.Rewrites[0].To)
	}
}

// An override can point a job at a pool for a label the consolidated mapping
// left alone. That is the whole reason it is per job rather than per label: a
// fleet with no Windows pool can still move the one Windows job it has room for.
func TestOverrideMigratesALabelNobodyMapped(t *testing.T) {
	in := "jobs:\n  windows:\n    runs-on: windows-latest\n"
	m := zoomies
	m.Overrides = []Override{{
		Repo: "acme/widgets", Path: ".github/workflows/ci.yml", Job: "windows", To: "zoomies-windows-x64",
	}}

	got := File(in, m.In("acme/widgets", ".github/workflows/ci.yml"))
	if len(got.Rewrites) != 1 || got.Rewrites[0].To != "zoomies-windows-x64" {
		t.Fatalf("rewrites = %+v, want the Windows job on the override's pool", got.Rewrites)
	}
	if got.Rewrites[0].Label != "windows-latest" {
		t.Errorf("label = %q, want the hosted-runner label it replaced", got.Rewrites[0].Label)
	}
}

// The other direction: everything moves except one job, which an operator has a
// reason to leave where it is. An empty target is a decision, and it says so.
func TestOverrideCanPinAJobWhereItIs(t *testing.T) {
	in := "jobs:\n  build:\n    runs-on: ubuntu-latest\n  flaky:\n    runs-on: ubuntu-latest\n"
	m := zoomies
	m.Overrides = []Override{{
		Repo: "acme/widgets", Path: ".github/workflows/ci.yml", Job: "flaky", To: "",
	}}

	got := File(in, m.In("acme/widgets", ".github/workflows/ci.yml"))
	if len(got.Rewrites) != 1 || got.Rewrites[0].Job != "build" {
		t.Fatalf("rewrites = %+v, want only the job that was not pinned", got.Rewrites)
	}
	if len(got.Skips) != 1 || got.Skips[0].Job != "flaky" {
		t.Fatalf("skips = %+v, want the pinned job", got.Skips)
	}
	// "not mapped" would send the operator off to fix a mapping that is fine.
	if strings.Contains(got.Skips[0].Reason, "not mapped") {
		t.Errorf("reason = %q, want it to say the job was left where it is deliberately", got.Skips[0].Reason)
	}
	// The reason names the label rather than GitHub, because a pinned job may
	// be on a vendor's runners rather than GitHub's.
	if !strings.Contains(got.Skips[0].Reason, "stay on ubuntu-latest") {
		t.Errorf("reason = %q, want it to name the decision and the label", got.Skips[0].Reason)
	}
}

// A mapping that was never narrowed carries its overrides but has applied none
// of them. It must fall back to the consolidated answer rather than reach for
// somebody else's exception.
func TestOverridesDoNothingUntilTheMappingIsNarrowed(t *testing.T) {
	in := "jobs:\n  build:\n    runs-on: ubuntu-latest\n"
	m := zoomies
	m.Overrides = []Override{{
		Repo: "acme/widgets", Path: ".github/workflows/ci.yml", Job: "build", To: "zoomies-big",
	}}
	if got := File(in, m); got.Rewrites[0].To != "zoomies-linux-x64" {
		t.Fatalf("to = %q, want the consolidated mapping", got.Rewrites[0].To)
	}
}

// The block-sequence form is rewritten through the same path, so an override
// has to reach it too.
func TestOverrideReachesABlockSequence(t *testing.T) {
	in := "jobs:\n  build:\n    runs-on:\n      - ubuntu-latest\n    steps: []\n"
	m := zoomies
	m.Overrides = []Override{{
		Repo: "acme/widgets", Path: ".github/workflows/ci.yml", Job: "build", To: "zoomies-big",
	}}

	got := File(in, m.In("acme/widgets", ".github/workflows/ci.yml"))
	want := "jobs:\n  build:\n    runs-on: zoomies-big\n    steps: []\n"
	if got.Content != want {
		t.Fatalf("content = %q, want %q", got.Content, want)
	}
	if !got.Rewrites[0].Overridden {
		t.Error("a block sequence rewritten by an override is not marked as one")
	}
}

// A skip carries the label it asked for only when there is exactly one, which is
// what tells the wizard whether choosing a pool for that job would settle it. A
// ${{ }} expression is not a decision an override can make.
func TestSkipsSayWhetherAnOverrideCouldSettleThem(t *testing.T) {
	in := `jobs:
  unmapped:
    runs-on: windows-latest
  computed:
    runs-on: ${{ matrix.os }}
  taken:
    runs-on: [self-hosted, linux]
`
	got := File(in, zoomies)
	if len(got.Skips) != 3 {
		t.Fatalf("skips = %+v, want three", got.Skips)
	}
	byJob := map[string]Skip{}
	for _, s := range got.Skips {
		byJob[s.Job] = s
	}
	if byJob["unmapped"].Label != "windows-latest" {
		t.Errorf("the unmapped job carries label %q, want the one an override would replace", byJob["unmapped"].Label)
	}
	for _, job := range []string{"computed", "taken"} {
		if byJob[job].Label != "" {
			t.Errorf("%s carries label %q, want none: no override can settle it", job, byJob[job].Label)
		}
	}
}

// An override needs a job to name. A runs-on the tracker could not attribute has
// no stable key, and one with an empty job would otherwise match every such line
// in the file.
func TestAnOverrideWithNoJobIsIgnored(t *testing.T) {
	m := Mapping{Overrides: []Override{{Repo: "acme/widgets", Path: "p.yml", Job: "  ", To: "zoomies-big"}}}
	if got := m.In("acme/widgets", "p.yml"); len(got.jobs) != 0 {
		t.Fatalf("jobs = %v, want an override with no job to be dropped", got.jobs)
	}
}
