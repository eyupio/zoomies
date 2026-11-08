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

// scanWith is ScanWith for a test, with local actions served from a map of
// their directories to their action.yml, and a record of which were asked for.
func scanWith(t *testing.T, content string, actions map[string]string) ([]Requirement, []string) {
	t.Helper()
	var asked []string
	got, err := ScanWith(".github/workflows/ci.yml", content, Options{ReadLocal: func(dir string) (string, bool) {
		asked = append(asked, dir)
		c, ok := actions[dir]
		return c, ok
	}})
	if err != nil {
		t.Fatalf("ScanWith: %v", err)
	}
	return got, asked
}

// A workflow that writes its Node version once, in env, and passes it to every
// setup-node step is the ordinary way to keep versions in step.
func TestScanReadsAVersionFromTheWorkflowsEnv(t *testing.T) {
	got := scan(t, `
env:
  NODE_VERSION: "22"
  GO_VERSION: "1.26"
jobs:
  web:
    runs-on: linux
    env:
      GO_VERSION: "1.27"
    steps:
      - uses: actions/setup-node@v5
        with:
          node-version: ${{ env.NODE_VERSION }}
      - uses: actions/setup-go@v6
        with:
          go-version: ${{ env.GO_VERSION }}
      - uses: actions/setup-python@v6
        env:
          PY: "3.13"
        with:
          python-version: ${{ env.py }}
`)
	want := []string{"web node 22", "web go 1.27", "web python 3.13"}
	if !reflect.DeepEqual(brief(got), want) {
		t.Fatalf("got %q\nwant %q\n(a job's env wins over the workflow's, a step's over both, and a name is read without regard to case)", brief(got), want)
	}
}

func TestScanSaysWhyAnEnvValueIsUnresolved(t *testing.T) {
	got := scan(t, `
env:
  FROM_A_SECRET: ${{ secrets.NODE }}
jobs:
  a:
    runs-on: linux
    steps:
      - uses: actions/setup-node@v5
        with:
          node-version: ${{ env.MISSING }}
      - uses: actions/setup-node@v5
        with:
          node-version: ${{ env.FROM_A_SECRET }}
`)
	want := []string{
		"a node ? set by env.MISSING, which the workflow does not set",
		"a node ? set by the expression ${{ secrets.NODE }}",
	}
	if !reflect.DeepEqual(brief(got), want) {
		t.Fatalf("got %q\nwant %q", brief(got), want)
	}
}

func TestScanFollowsAMatrixIntoAnEnvValue(t *testing.T) {
	got := scan(t, `
jobs:
  a:
    runs-on: linux
    strategy:
      matrix:
        v: ["20", "22"]
    env:
      NODE_VERSION: ${{ matrix.v }}
    steps:
      - uses: actions/setup-node@v5
        with:
          node-version: ${{ env.NODE_VERSION }}
`)
	if want := []string{"a node 20", "a node 22"}; !reflect.DeepEqual(brief(got), want) {
		t.Fatalf("got %q, want %q", brief(got), want)
	}
}

// The shape this repository's own CI has: the version is pinned once, inside a
// local composite action every job calls, so the workflow never names it.
const goAction = `
name: Set up Go
runs:
  using: composite
  steps:
    - uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0
      with:
        go-version: "1.27.3"
        cache: true
`

func TestScanFollowsALocalCompositeActionToTheVersionItPins(t *testing.T) {
	got, asked := scanWith(t, `
jobs:
  test:
    runs-on: linux
    steps:
      - uses: actions/checkout@v7
      - uses: ./.github/actions/setup-go
  vet:
    runs-on: linux
    steps:
      - uses: ./.github/actions/setup-go
`, map[string]string{".github/actions/setup-go": goAction})
	if want := []string{"test go 1.27.3", "vet go 1.27.3"}; !reflect.DeepEqual(brief(got), want) {
		t.Fatalf("got %q, want %q", brief(got), want)
	}
	if want := []string{".github/actions/setup-go"}; !reflect.DeepEqual(asked, want) {
		t.Fatalf("asked for %q: an action is read once, however many jobs use it, and actions/checkout is not local", asked)
	}
}

func TestScanPassesAStepsInputsIntoALocalAction(t *testing.T) {
	got, _ := scanWith(t, `
env:
  FALLBACK: "1.25"
jobs:
  a:
    runs-on: linux
    strategy:
      matrix:
        go: ["1.26", "1.27"]
    steps:
      - uses: ./.github/actions/go
        with:
          version: ${{ matrix.go }}
  b:
    runs-on: linux
    steps:
      - uses: ./.github/actions/go
  c:
    runs-on: linux
    steps:
      - uses: ./.github/actions/go
        with:
          version: ${{ steps.pick.outputs.go }}
`, map[string]string{".github/actions/go": `
inputs:
  version:
    default: ${{ env.FALLBACK }}
runs:
  using: composite
  steps:
    - uses: actions/setup-go@v6
      with:
        go-version: ${{ inputs.version }}
`})
	want := []string{
		"a go 1.26",
		"a go 1.27",
		"b go 1.25",
		"c go ? set by the expression ${{ steps.pick.outputs.go }}",
	}
	if !reflect.DeepEqual(brief(got), want) {
		t.Fatalf("got %q\nwant %q", brief(got), want)
	}
}

func TestScanSaysWhenALocalActionNamesAnInputItDoesNotDeclare(t *testing.T) {
	got, _ := scanWith(t, `
jobs:
  a:
    runs-on: linux
    steps:
      - uses: ./act
`, map[string]string{"act": `
runs:
  using: composite
  steps:
    - uses: actions/setup-go@v6
      with:
        go-version: ${{ inputs.nope }}
`})
	want := []string{"a go ? set by inputs.nope, which the local action does not declare"}
	if !reflect.DeepEqual(brief(got), want) {
		t.Fatalf("got %q, want %q", brief(got), want)
	}
}

func TestScanFollowsLocalActionsThroughEachOtherButNotForever(t *testing.T) {
	// act0 calls act1 calls act2 calls act3, which installs Go: three hops are
	// followed and a fourth is not.
	chain := map[string]string{}
	for i := 0; i < 4; i++ {
		next := "./act" + string(rune('0'+i+1))
		inner := "    - uses: " + next
		if i == 3 {
			inner = "    - uses: actions/setup-go@v6\n      with:\n        go-version: \"1.27\""
		}
		chain["act"+string(rune('0'+i))] = "runs:\n  using: composite\n  steps:\n" + inner + "\n"
	}
	got, _ := scanWith(t, "jobs:\n  a:\n    runs-on: linux\n    steps:\n      - uses: ./act0\n", chain)
	if len(got) != 0 {
		t.Fatalf("followed a fourth local action: %q", brief(got))
	}
	// One hop fewer, and it is found.
	chain["act2"] = "runs:\n  using: composite\n  steps:\n    - uses: actions/setup-go@v6\n      with:\n        go-version: \"1.27\"\n"
	got, _ = scanWith(t, "jobs:\n  a:\n    runs-on: linux\n    steps:\n      - uses: ./act0\n", chain)
	if want := []string{"a go 1.27"}; !reflect.DeepEqual(brief(got), want) {
		t.Fatalf("got %q, want %q", brief(got), want)
	}
}

func TestScanStopsAtALocalActionThatCallsItself(t *testing.T) {
	got, asked := scanWith(t, "jobs:\n  a:\n    runs-on: linux\n    steps:\n      - uses: ./loop\n", map[string]string{
		"loop": "runs:\n  using: composite\n  steps:\n    - uses: ./loop\n",
	})
	if len(got) != 0 || len(asked) != 1 {
		t.Fatalf("got %q after asking for %q", brief(got), asked)
	}
}

// Without the means to read a local action, a workflow is read as it always
// was, and a local action that cannot be read is not a setup action and not a
// complaint: most local actions have nothing to do with a toolchain.
func TestScanLeavesALocalActionAloneWhenItCannotBeRead(t *testing.T) {
	const wf = "jobs:\n  a:\n    runs-on: linux\n    steps:\n      - uses: ./.github/actions/setup-go\n"
	if got := scan(t, wf); len(got) != 0 {
		t.Fatalf("Scan followed a local action: %q", brief(got))
	}
	got, asked := scanWith(t, wf, nil)
	if len(got) != 0 || len(asked) != 1 {
		t.Fatalf("got %q after asking for %q", brief(got), asked)
	}
}

func TestScanIgnoresALocalActionThatIsNotComposite(t *testing.T) {
	got, _ := scanWith(t, "jobs:\n  a:\n    runs-on: linux\n    steps:\n      - uses: ./js\n", map[string]string{
		"js": "runs:\n  using: node20\n  main: index.js\n",
	})
	if len(got) != 0 {
		t.Fatalf("got %q", brief(got))
	}
}

func TestScanDoesNotReadAPathOutsideTheRepository(t *testing.T) {
	_, asked := scanWith(t, `
jobs:
  a:
    runs-on: linux
    steps:
      - uses: ./../outside
      - uses: ./a/../../outside
      - uses: ./ok/../fine
`, map[string]string{})
	if want := []string{"fine"}; !reflect.DeepEqual(asked, want) {
		t.Fatalf("asked for %q, want only %q: a path that climbs out of the repository is not a local action", asked, want)
	}
}

func TestScanReadsALocalActionAtTheRepositoryRoot(t *testing.T) {
	got, asked := scanWith(t, "jobs:\n  a:\n    runs-on: linux\n    steps:\n      - uses: ./\n", map[string]string{
		".": goAction,
	})
	if want := []string{"a go 1.27.3"}; !reflect.DeepEqual(brief(got), want) || !reflect.DeepEqual(asked, []string{"."}) {
		t.Fatalf("got %q after asking for %q", brief(got), asked)
	}
}
