package toolscan

import (
	"reflect"
	"testing"
)

// scan is Scan for a test: a file that does not parse is the test's mistake.
func scan(t *testing.T, content string) []Requirement {
	t.Helper()
	got, err := Scan(".github/workflows/ci.yml", content)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	return got
}

// brief is what most tests compare: the job, its labels, the tool and the
// version, one string per requirement.
func brief(rs []Requirement) []string {
	var out []string
	for _, r := range rs {
		s := r.Job + " " + string(r.Tool) + " "
		if r.Unresolved != "" {
			s += "? " + r.Unresolved
		} else {
			s += r.Version
		}
		if r.Distribution != "" {
			s += " (" + r.Distribution + ")"
		}
		out = append(out, s)
	}
	return out
}

func TestScanReadsEverySetupAction(t *testing.T) {
	got := scan(t, `
jobs:
  build:
    runs-on: [self-hosted, linux]
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-python@v6
        with:
          python-version: "3.12"
      - uses: actions/setup-node@v5
        with:
          node-version: 22.x
      - uses: actions/setup-go@v6
        with:
          go-version: "1.27"
      - uses: actions/setup-java@v5
        with:
          distribution: temurin
          java-version: 21
      - uses: actions/setup-dotnet@v5
        with:
          dotnet-version: 8.0.x
`)
	want := []string{
		"build python 3.12",
		"build node 22.x",
		"build go 1.27",
		"build java 21 (temurin)",
		"build dotnet 8.0.x",
	}
	if !reflect.DeepEqual(brief(got), want) {
		t.Fatalf("got %q\nwant %q", brief(got), want)
	}
	for _, r := range got {
		if !reflect.DeepEqual(r.Labels, []string{"self-hosted", "linux"}) {
			t.Errorf("%s: labels %q", r.Tool, r.Labels)
		}
	}
}

// An unquoted 3.10 is the float 3.1 to a YAML decoder, and the version a
// workflow asks for is the text GitHub hands the action. Reading it as a
// number would report Python 3.1, which nobody asked for.
func TestScanKeepsAVersionAsItIsWritten(t *testing.T) {
	got := scan(t, `
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/setup-python@v6
        with:
          python-version: 3.10
`)
	if want := []string{"test python 3.10"}; !reflect.DeepEqual(brief(got), want) {
		t.Fatalf("got %q, want %q", brief(got), want)
	}
}

// A matrix is how most workflows ask for several versions, and the labels are
// often in it too; each combination is a job GitHub runs.
func TestScanExpandsAMatrix(t *testing.T) {
	got := scan(t, `
jobs:
  test:
    runs-on: ${{ matrix.runner }}
    strategy:
      matrix:
        runner: [linux-x64, linux-arm64]
        python: ["3.11", "3.12"]
        exclude:
          - runner: linux-arm64
            python: "3.11"
    steps:
      - uses: actions/setup-python@v6
        with:
          python-version: ${{ matrix.python }}
`)
	var labelled []string
	for _, r := range got {
		labelled = append(labelled, r.Labels[0]+" "+r.Version)
	}
	want := []string{"linux-x64 3.11", "linux-x64 3.12", "linux-arm64 3.12"}
	if !reflect.DeepEqual(labelled, want) {
		t.Fatalf("got %q, want %q", labelled, want)
	}
}

// include adds a key to the combinations it matches, and a combination of its
// own when it matches none; a matrix of nothing but includes is a list of jobs.
func TestScanFollowsMatrixInclude(t *testing.T) {
	got := scan(t, `
jobs:
  test:
    runs-on: linux
    strategy:
      matrix:
        include:
          - node: "22"
          - node: "24"
            experimental: true
    steps:
      - uses: actions/setup-node@v5
        with:
          node-version: ${{ matrix.node }}
`)
	if want := []string{"test node 22", "test node 24"}; !reflect.DeepEqual(brief(got), want) {
		t.Fatalf("got %q, want %q", brief(got), want)
	}
}

// setup-python, setup-java and setup-dotnet each take several versions, one
// per line; every one of them is a download.
func TestScanSplitsAMultilineVersionInput(t *testing.T) {
	got := scan(t, `
jobs:
  test:
    runs-on: linux
    steps:
      - uses: actions/setup-dotnet@v5
        with:
          dotnet-version: |
            8.0.x
            10.0.x
`)
	if want := []string{"test dotnet 8.0.x", "test dotnet 10.0.x"}; !reflect.DeepEqual(brief(got), want) {
		t.Fatalf("got %q, want %q", brief(got), want)
	}
}

// What is not in the file is said to be not in the file. Dropping it would read
// as "this job needs no Python", and guessing would prefill the wrong version.
func TestScanSaysWhyAVersionIsUnresolved(t *testing.T) {
	got := scan(t, `
jobs:
  a:
    runs-on: linux
    steps:
      - uses: actions/setup-go@v6
        with:
          go-version-file: go.mod
  b:
    runs-on: linux
    steps:
      - uses: actions/setup-node@v5
        with:
          node-version: ${{ inputs.node }}
  c:
    runs-on: linux
    strategy:
      matrix: ${{ fromJSON(needs.plan.outputs.matrix) }}
    steps:
      - uses: actions/setup-python@v6
        with:
          python-version: ${{ matrix.python }}
`)
	want := []string{
		"a go ? read from go.mod in the repository",
		"b node ? set by the expression ${{ inputs.node }}",
		"c python ? set by a matrix built at run time",
	}
	if !reflect.DeepEqual(brief(got), want) {
		t.Fatalf("got %q\nwant %q", brief(got), want)
	}
}

// Labels that come from an expression the file cannot answer are unknown, not
// empty: an empty list would match any pool.
func TestScanLeavesUnknowableLabelsNil(t *testing.T) {
	got := scan(t, `
jobs:
  a:
    runs-on: ${{ inputs.runner }}
    steps:
      - uses: actions/setup-go@v6
        with:
          go-version: "1.27"
`)
	if len(got) != 1 || got[0].Labels != nil {
		t.Fatalf("got %+v, want one requirement with nil labels", got)
	}
}

// runs-on as a mapping names a runner group and labels; the group is not a
// label.
func TestScanReadsLabelsFromARunnerGroupMapping(t *testing.T) {
	got := scan(t, `
jobs:
  a:
    runs-on:
      group: builders
      labels: [linux, x64]
    steps:
      - uses: actions/setup-go@v6
        with:
          go-version: "1.27"
`)
	if len(got) != 1 || !reflect.DeepEqual(got[0].Labels, []string{"linux", "x64"}) {
		t.Fatalf("got %+v", got)
	}
}

// GitHub resolves uses: case-insensitively, and pinning by commit is the norm
// in a careful repository -- neither should hide a step.
func TestScanRecognisesPinnedAndOddlyCasedActions(t *testing.T) {
	got := scan(t, `
jobs:
  a:
    runs-on: linux
    steps:
      - uses: Actions/Setup-Go@4d34df0c2316fe8122ab82dc22947d607c0c91f9 # v5
        with:
          go-version: "1.26"
      - uses: ./local/setup-go
      - uses: someone/setup-python@v1
        with:
          python-version: "3.12"
`)
	if want := []string{"a go 1.26"}; !reflect.DeepEqual(brief(got), want) {
		t.Fatalf("got %q, want %q", brief(got), want)
	}
}

// The same requirement from two steps, or two combinations that only differ in
// a key the step does not use, is one requirement.
func TestScanReportsARequirementOnce(t *testing.T) {
	got := scan(t, `
jobs:
  a:
    runs-on: linux
    strategy:
      matrix:
        shard: [1, 2, 3]
    steps:
      - uses: actions/setup-node@v5
        with:
          node-version: "24"
`)
	if want := []string{"a node 24"}; !reflect.DeepEqual(brief(got), want) {
		t.Fatalf("got %q, want %q", brief(got), want)
	}
}

func TestScanRefusesAFileThatIsNotYAML(t *testing.T) {
	if _, err := Scan("bad.yml", "jobs: [\n"); err == nil {
		t.Fatal("expected an error")
	}
}
