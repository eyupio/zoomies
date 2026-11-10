package docs

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/eyupio/zoomies/internal/updates"
)

// changesScript is the shell that decides what CI runs for a change, taken
// out of ci.yml so the decisions can be asked about directly.
func changesScript(t *testing.T) string {
	t.Helper()
	var wf struct {
		Jobs map[string]struct {
			Steps []struct {
				ID  string `yaml:"id"`
				Run string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(workflowFiles(t)["ci.yml"]), &wf); err != nil {
		t.Fatalf("parsing ci.yml: %v", err)
	}
	for _, step := range wf.Jobs["changes"].Steps {
		if step.ID == "filter" {
			return step.Run
		}
	}
	t.Fatal("ci.yml's changes job has no filter step")
	return ""
}

// decide runs the filter as a push to main, or a pull request, that changed
// these files, with a git that answers only the two questions it asks.
func decide(t *testing.T, event string, files ...string) map[string]string {
	t.Helper()
	dir := t.TempDir()
	stub := "#!/bin/sh\ncase \"$1\" in fetch) exit 0 ;; diff) printf '%s\\n' " + strings.Join(files, " ") + " ;; esac\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "output")
	ref := "refs/pull/1/merge"
	if event == "push" {
		ref = "refs/heads/main"
	}
	cmd := exec.Command("sh", "-c", changesScript(t))
	cmd.Env = append(os.Environ(),
		"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"EVENT="+event, "REF="+ref, "BASE=0123abc", "GITHUB_OUTPUT="+out)
	if msg, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the filter failed for %v: %v\n%s", files, err, msg)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			got[k] = v
		}
	}
	return got
}

// A merge to main used to run everything, so a README edit rebuilt and
// republished every binary and image on the dev channel with nothing in them
// different, and held the fleet for an hour doing it. What a change runs is
// now what its files can affect, and only a change to what ships publishes.
func TestCIRunsOnlyWhatAChangeCanAffect(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the filter is POSIX shell, and runs on Linux in CI")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh to run the filter with")
	}
	all := `["controller-1","controller-2","api-1","api-2","store","rest"]`
	for _, tc := range []struct {
		name  string
		event string
		files []string
		want  map[string]string
	}{
		{"the README checks its own facts and nothing else", "push", []string{"README.md"},
			map[string]string{"go": "false", "web": "false", "deploy": "false", "installer": "false", "shards": `["docs"]`, "publish": "false"}},
		// internal/docs holds install.sh's release key to the binary's and its
		// verifier to the other scripts', and its whole-tree scans read every
		// file, so a change to a script people curl runs that package.
		{"the install script is checked and upgraded through, and read by the docs tests, not rebuilt into binaries or images", "push", []string{"install.sh"},
			map[string]string{"go": "false", "deploy": "false", "installer": "true", "shards": `["docs"]`, "publish": "false"}},
		{"the Windows installer is held to the same tests", "push", []string{"install.ps1"},
			map[string]string{"go": "false", "deploy": "false", "installer": "true", "shards": `["docs"]`, "publish": "false"}},
		{"the install and upgrade checks are shell the whole-tree scans read", "push", []string{"test/install/install-check.sh", "test/upgrade/upgrade-check.sh"},
			map[string]string{"go": "false", "deploy": "false", "installer": "true", "shards": `["docs"]`, "publish": "false"}},
		// internal/proxmoxsetup holds its signature check equal to the one the
		// wizard's command carries, and that package is in the rest shard.
		{"the Proxmox connector is run through the tests that hold its signature check to the others", "pull_request", []string{"connect-proxmox.sh"},
			map[string]string{"go": "false", "deploy": "false", "installer": "true", "shards": `["rest"]`, "publish": "false"}},
		{"a README next to the runner image check is prose, and builds no image", "push", []string{"test/runner-image/README.md"},
			map[string]string{"go": "false", "deploy": "false", "installer": "false", "shards": `["docs"]`, "publish": "false"}},
		{"the runner image check itself still builds the images and is read by the whole-tree scans", "pull_request", []string{"test/runner-image/check.sh"},
			map[string]string{"go": "false", "deploy": "true", "installer": "true", "shards": `["docs"]`}},
		{"a README in deploy/ is prose, and publishes nothing", "push", []string{"deploy/marketplace/README.md"},
			map[string]string{"go": "false", "deploy": "false", "installer": "false", "shards": `["docs"]`, "publish": "false"}},
		{"a file nothing reads runs nothing", "push", []string{"LICENSE", ".gitignore"},
			map[string]string{"go": "false", "installer": "false", "shards": "[]", "publish": "false"}},
		{"Go on main is the whole suite and a publish", "push", []string{"internal/store/queries.go"},
			map[string]string{"go": "true", "shards": all, "publish": "true"}},
		{"the UI is embedded in the binary, so it publishes too", "push", []string{"web/src/lib/units.ts"},
			map[string]string{"go": "false", "web": "true", "publish": "true"}},
		{"an image input publishes, so the binaries and images keep naming one commit", "push", []string{"deploy/Dockerfile"},
			map[string]string{"deploy": "true", "shards": `["rest"]`, "publish": "true"}},
		{"the InstaPods scripts are linted and tested, and go into no image", "push", []string{"deploy/instapods/run.sh"},
			map[string]string{"go": "false", "deploy": "false", "installer": "true", "shards": `["rest"]`, "publish": "false"}},
		{"the package-cleanup scripts are linted and read as prose, and ship nothing", "push", []string{".github/scripts/ghcr-prune.sh"},
			map[string]string{"go": "false", "deploy": "false", "installer": "true", "shards": `["docs"]`, "publish": "false"}},
		{"the marketplace bootstrap is linted, tested and part of the image", "push", []string{"deploy/marketplace/bootstrap.sh"},
			map[string]string{"go": "false", "deploy": "true", "installer": "true", "shards": `["rest"]`, "publish": "true"}},
		{"the tool-cache filler is embedded in the binary and linted as shell", "push", []string{"internal/backend/toolfill.sh"},
			map[string]string{"go": "true", "installer": "true", "shards": all, "publish": "true"}},
		{"the demo organisation's workflow templates are prose to this repository", "push", []string{"deploy/demo-org/build.yml"},
			map[string]string{"go": "false", "deploy": "false", "installer": "false", "shards": `["docs"]`, "publish": "false"}},
		{"a pull request never publishes", "pull_request", []string{"internal/store/queries.go"},
			map[string]string{"go": "true", "publish": "false"}},
		{"a change to CI itself runs everything", "pull_request", []string{".github/workflows/ci.yml"},
			map[string]string{"go": "true", "web": "true", "deploy": "true", "installer": "true", "shards": all}},
		{"an unknown kind of file runs too much rather than too little", "pull_request", []string{"something/new.xyz"},
			map[string]string{"go": "true", "shards": all}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := decide(t, tc.event, tc.files...)
			for k, want := range tc.want {
				if got[k] != want {
					t.Errorf("%s for %v: %s=%s, want %s (all: %v)", tc.event, tc.files, k, got[k], want, got)
				}
			}
		})
	}
}

// runnerImageScript is the shell the image-plan job runs to decide whether to build
// the runner images, taken out of ci.yml like the filter above. GitHub fills in
// ${{ github.event_name }} before a shell sees the script, so it is filled in
// here as a pull request.
func runnerImageScript(t *testing.T) string {
	t.Helper()
	var wf struct {
		Jobs map[string]struct {
			Steps []struct {
				ID  string `yaml:"id"`
				Run string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(workflowFiles(t)["ci.yml"]), &wf); err != nil {
		t.Fatalf("parsing ci.yml: %v", err)
	}
	for _, step := range wf.Jobs["image-plan"].Steps {
		if step.ID == "changed" {
			return strings.ReplaceAll(step.Run, "${{ github.event_name }}", "pull_request")
		}
	}
	t.Fatal("ci.yml's image-plan job has no step with the id changed")
	return ""
}

// buildsRunnerImages runs that step for a pull request that changed these
// files, with a git that answers only the two questions it asks, and says
// whether it chose to build the runner images.
func buildsRunnerImages(t *testing.T, files ...string) bool {
	t.Helper()
	dir := t.TempDir()
	stub := "#!/bin/sh\ncase \"$1\" in cat-file) exit 0 ;; diff) printf '%s\\n' " + strings.Join(files, " ") + " ;; esac\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "output")
	cmd := exec.Command("sh", "-c", runnerImageScript(t))
	cmd.Env = append(os.Environ(),
		"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"BASE=0123abc", "PUBLISH=false", "GITHUB_OUTPUT="+out)
	if msg, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the step failed for %v: %v\n%s", files, err, msg)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if v, ok := strings.CutPrefix(line, "runner="); ok {
			return v == "true"
		}
	}
	t.Fatalf("the step wrote no runner output for %v: %q", files, raw)
	return false
}

// The weekly base-image refresh opens a pull request that moves digests in
// internal/naming/images.go and regenerates what is derived from it, and its
// description says CI builds every variant on that pull request. The step only
// looked under deploy/ and test/runner-image, so that pull request built no
// runner image and a digest nobody had built went unproven. The tool-cache lock
// is copied into every image, and was missed the same way.
func TestTheRunnerImagesAreBuiltWhenAnythingTheyAreMadeFromChanges(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the step is POSIX shell, and runs on Linux in CI")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh to run the step with")
	}
	for _, tc := range []struct {
		name  string
		files []string
		want  bool
	}{
		{"the weekly base refresh moves digests, and the files generated from them", []string{
			"internal/naming/images.go", "Makefile", ".github/workflows/ci.yml", ".github/workflows/release.yml", "docs/naming.md"}, true},
		{"the tool-cache lock is copied into every image", []string{"deploy/toolcache.lock"}, true},
		{"the runner Dockerfile", []string{"deploy/Dockerfile.runner"}, true},
		{"a script the runner Dockerfile copies in", []string{"deploy/runner-apt.sh"}, true},
		{"the runner entrypoint", []string{"deploy/runner-entrypoint.sh"}, true},
		{"the check each built image is run through", []string{"test/runner-image/check.sh"}, true},
		{"prose about the images", []string{"docs/naming.md"}, false},
		{"the catalogue's tests and its generators build nothing", []string{"internal/naming/images_test.go", "internal/naming/gen_base_digests.go"}, false},
		{"the lock's generator is not the lock", []string{"deploy/gen_toolcache_lock.go"}, false},
		{"the controller image is not a runner image", []string{"deploy/Dockerfile"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := buildsRunnerImages(t, tc.files...); got != tc.want {
				t.Errorf("a pull request changing %v: runner=%v, want %v", tc.files, got, tc.want)
			}
		})
	}
}

// changesStep is one step of the changes job as ci.yml spells it.
type changesStep struct {
	ID  string            `yaml:"id"`
	If  string            `yaml:"if"`
	Run string            `yaml:"run"`
	Env map[string]string `yaml:"env"`
}

// changesJob is the changes job's steps, and the permissions it asks for.
func changesJob(t *testing.T) ([]changesStep, map[string]string) {
	t.Helper()
	var wf struct {
		Jobs map[string]struct {
			Permissions map[string]string `yaml:"permissions"`
			Steps       []changesStep     `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(workflowFiles(t)["ci.yml"]), &wf); err != nil {
		t.Fatalf("parsing ci.yml: %v", err)
	}
	job := wf.Jobs["changes"]
	return job.Steps, job.Permissions
}

// changesStepWithID finds one of the changes job's steps.
func changesStepWithID(t *testing.T, id string) changesStep {
	t.Helper()
	steps, _ := changesJob(t)
	for _, step := range steps {
		if step.ID == id {
			return step
		}
	}
	t.Fatalf("ci.yml's changes job has no step with the id %s", id)
	return changesStep{}
}

// outputsOf reads the name=value lines a step wrote to $GITHUB_OUTPUT.
func outputsOf(t *testing.T, path string) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			got[k] = v
		}
	}
	return got
}

// finishedRun is the part of a workflow run that the base step reads.
type finishedRun struct {
	Number     int    `json:"run_number"`
	Event      string `json:"event"`
	Conclusion string `json:"conclusion"`
	Head       string `json:"head_sha"`
}

// noBase is the commit a push with nothing to diff against is given. The
// filter answers it by running everything.
const noBase = "0000000000000000000000000000000000000000"

// baseOfPush runs the step that names the commit a push to main is diffed
// against, as run number n, with a gh that answers the runs endpoint from
// these finished runs through the --jq it is handed, so the expression ci.yml
// carries is the one tested. A refused gh fails as one without the permission
// does. It returns the commit the step chose and the arguments it gave gh.
func baseOfPush(t *testing.T, n int, runs []finishedRun, refused bool) (sha, ghArgs string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the step is POSIX shell, and runs on Linux in CI")
	}
	for _, tool := range []string{"sh", "jq"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("no %s to run the step with", tool)
		}
	}
	dir := t.TempDir()
	body, err := json.Marshal(map[string]any{"workflow_runs": runs})
	if err != nil {
		t.Fatal(err)
	}
	stub := "#!/bin/sh\n" +
		"echo \"$*\" >> \"$GH_LOG\"\n" +
		"expr=\n" +
		"while [ $# -gt 0 ]; do case \"$1\" in --jq) expr=$2; shift ;; esac; shift; done\n" +
		"[ -z \"${GH_REFUSED:-}\" ] || { echo 'gh: HTTP 403: Resource not accessible by integration' >&2; exit 1; }\n" +
		"exec jq -r \"$expr\" \"$GH_RUNS\"\n"
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "runs.json"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	out, log := filepath.Join(dir, "output"), filepath.Join(dir, "gh.log")
	cmd := exec.Command("sh", "-c", changesStepWithID(t, "base").Run)
	cmd.Env = append(os.Environ(),
		"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GITHUB_REPOSITORY=eyupio/zoomies",
		"GITHUB_WORKFLOW_REF=eyupio/zoomies/.github/workflows/ci.yml@refs/heads/main",
		"RUN_NUMBER="+strconv.Itoa(n), "GITHUB_OUTPUT="+out,
		"GH_RUNS="+filepath.Join(dir, "runs.json"), "GH_LOG="+log)
	if refused {
		cmd.Env = append(cmd.Env, "GH_REFUSED=1")
	}
	if msg, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the base step failed: %v\n%s", err, msg)
	}
	args, _ := os.ReadFile(log)
	return outputsOf(t, out)["sha"], string(args)
}

// decidePush runs the filter as a push to main diffed against base, with a git
// that answers a diff from the files each base is said to differ from the
// pushed commit by.
func decidePush(t *testing.T, base string, diffs map[string][]string) map[string]string {
	t.Helper()
	var arms strings.Builder
	for from, files := range diffs {
		fmt.Fprintf(&arms, "%s) printf '%%s\\n' %s ;; ", from, strings.Join(files, " "))
	}
	dir := t.TempDir()
	stub := "#!/bin/sh\ncase \"$1\" in fetch) exit 0 ;; diff) case \"$3\" in " + arms.String() +
		"*) echo \"no diff from $3\" >&2; exit 1 ;; esac ;; esac\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "output")
	cmd := exec.Command("sh", "-c", changesScript(t))
	cmd.Env = append(os.Environ(),
		"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"EVENT=push", "REF=refs/heads/main", "BASE="+base, "GITHUB_OUTPUT="+out)
	if msg, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the filter failed against %s: %v\n%s", base, err, msg)
	}
	return outputsOf(t, out)
}

// Main queues its runs and keeps only the newest of those still waiting, so the
// run for a push that another push overtook is cancelled before it starts. The
// run that overtook it used to be diffed against the push before its own, and
// the files of the one it replaced went unseen: a Go change merged a minute
// before a docs change ran no Go shard on main, and a change to what ships was
// not published. The base is the last run that was not cancelled, so the newest
// run covers everything the ones it replaced would have.
func TestAPushToMainIsDiffedAgainstTheLastRunThatWasNotCancelled(t *testing.T) {
	for _, tc := range []struct {
		name    string
		number  int
		runs    []finishedRun
		refused bool
		want    string
	}{
		{"a run that replaced the ones waiting behind it goes back to the last that ran", 3725, []finishedRun{
			{3721, "push", "cancelled", "bc456e2"},
			{3718, "push", "cancelled", "824587c"},
			{3717, "push", "cancelled", "71f62ce"},
			{3712, "push", "cancelled", "023102b"},
			{3710, "push", "failure", "42aeebd"},
		}, false, "42aeebd"},
		{"a run that passed ran, and so did one that failed", 12, []finishedRun{
			{11, "push", "success", "bbb"}, {10, "push", "failure", "aaa"}}, false, "bbb"},
		{"a run that timed out ran", 12, []finishedRun{
			{11, "push", "timed_out", "bbb"}, {10, "push", "success", "aaa"}}, false, "bbb"},
		{"a run that never started anything did not", 12, []finishedRun{
			{11, "push", "startup_failure", "ccc"}, {10, "push", "skipped", "bbb"}, {9, "push", "success", "aaa"}}, false, "aaa"},
		{"only runs before this one count, so running it again sees what it saw the first time", 5, []finishedRun{
			{7, "push", "success", "ddd"}, {6, "push", "success", "ccc"}, {5, "push", "success", "bbb"}, {4, "push", "success", "aaa"}}, false, "aaa"},
		{"a run started by hand ran everything, and counts", 9, []finishedRun{
			{8, "workflow_dispatch", "success", "bbb"}, {7, "push", "success", "aaa"}}, false, "bbb"},
		{"a pull request from a fork's branch called main is not a run of main", 9, []finishedRun{
			{8, "pull_request", "success", "bbb"}, {7, "push", "success", "aaa"}}, false, "aaa"},
		{"only runs that were cancelled means run everything", 9, []finishedRun{{8, "push", "cancelled", "bbb"}}, false, noBase},
		{"no earlier run at all means run everything", 1, []finishedRun{}, false, noBase},
		{"an answer that cannot be had means run everything, not what the push before it changed", 9, []finishedRun{
			{8, "push", "success", "aaa"}}, true, noBase},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, args := baseOfPush(t, tc.number, tc.runs, tc.refused)
			if got != tc.want {
				t.Errorf("run %d: base=%s, want %s", tc.number, got, tc.want)
			}
			// Asking for every run would find a pull request's, and one still
			// going, and another workflow's.
			for _, want := range []string{"/actions/workflows/ci.yml/runs?", "branch=main", "status=completed"} {
				if !strings.Contains(args, want) {
					t.Errorf("the step asked gh %q, which lacks %q", args, want)
				}
			}
		})
	}
}

// The case that went wrong. Run 3717 carried a Go change and waited behind run
// 3710 while merges kept coming, and each cancelled the one before until 3725
// was the one left. Every merge but the first was a docs or site change, so
// 3725 was told it had changed docs, and ran the docs shard alone.
func TestTheRunThatReplacesCancelledOnesStillSeesTheirFiles(t *testing.T) {
	runs := []finishedRun{
		{3721, "push", "cancelled", "bc456e2"},
		{3718, "push", "cancelled", "824587c"},
		{3717, "push", "cancelled", "71f62ce"},
		{3712, "push", "cancelled", "023102b"},
		{3710, "push", "failure", "42aeebd"},
	}
	diffs := map[string][]string{
		"42aeebd": {"docs/ephemeral-runners.md", "internal/controller/controller.go", "docs/privacy.md", "hooks/seo.py"},
		"bc456e2": {"docs/privacy.md", "hooks/seo.py"},
	}
	all := `["controller-1","controller-2","api-1","api-2","store","rest"]`

	base, _ := baseOfPush(t, 3725, runs, false)
	got := decidePush(t, base, diffs)
	if got["go"] != "true" || got["shards"] != all || got["publish"] != "true" {
		t.Errorf("run 3725 was diffed against %s and chose %v; the Go change in run 3717 is the one nobody else will test", base, got)
	}

	// Diffed against the push before its own, which is what the filter was
	// given when it took github.event.before, it sees the docs alone. Were this
	// to pass without the base step, the scenario above would prove nothing.
	if before := decidePush(t, "bc456e2", diffs); before["shards"] != `["docs"]` {
		t.Errorf("against the push before its own the run chose %v, not the docs shard alone", before)
	}
}

// The filter reads its base from the step that names it, ahead of the push's
// own "before", and that step answers pushes to main and asks for the one
// permission listing a workflow's runs needs.
func TestTheFilterIsGivenTheBaseThatStepNames(t *testing.T) {
	base, filter := changesStepWithID(t, "base"), changesStepWithID(t, "filter")
	for _, want := range []string{"github.event_name == 'push'", "github.ref == 'refs/heads/main'"} {
		if !strings.Contains(base.If, want) {
			t.Errorf("the base step runs when %q, which lacks %q", base.If, want)
		}
	}
	named, before := strings.Index(filter.Env["BASE"], "steps.base.outputs.sha"), strings.Index(filter.Env["BASE"], "github.event.before")
	if named < 0 || before < 0 || named > before {
		t.Errorf("the filter's BASE is %q; the base step's answer has to come before the push's own before, or the files of a replaced run are lost again", filter.Env["BASE"])
	}
	if _, perms := changesJob(t); perms["actions"] != "read" {
		t.Errorf("the changes job asks for %v; listing a workflow's runs needs actions: read", perms)
	}
}

// ciNeeds reads each ci.yml job's needs, which GitHub lets be a single name or
// a list.
func ciNeeds(t *testing.T) map[string][]string {
	t.Helper()
	var wf struct {
		Jobs map[string]struct {
			Needs any `yaml:"needs"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(workflowFiles(t)["ci.yml"]), &wf); err != nil {
		t.Fatalf("parsing ci.yml: %v", err)
	}
	out := map[string][]string{}
	for name, job := range wf.Jobs {
		switch n := job.Needs.(type) {
		case string:
			out[name] = []string{n}
		case []any:
			for _, v := range n {
				if s, ok := v.(string); ok {
					out[name] = append(out[name], s)
				}
			}
		}
	}
	return out
}

// reaches reports whether job is waited on, directly or through other jobs, by
// from.
func reaches(needs map[string][]string, from, job string) bool {
	seen := map[string]bool{}
	var walk func(string) bool
	walk = func(n string) bool {
		for _, dep := range needs[n] {
			if dep == job {
				return true
			}
			if !seen[dep] {
				seen[dep] = true
				if walk(dep) {
					return true
				}
			}
		}
		return false
	}
	return walk(from)
}

// A host upgrades itself when the dev binaries change, and `zoomies upgrade`
// pulls the runner images it already holds as it does. With the binaries
// published first, a host could finish upgrading in the minutes before the
// runner image was rebuilt, pull the one about to be replaced, and keep it:
// that is how a pool moved back onto :dev started a runner two builds old. So
// the order is the runner images, then the controller, agent and Git worker
// images, then the binaries, and each step has to wait for the one before it.
func TestTheDevChannelPublishesRunnerImagesThenImagesThenBinaries(t *testing.T) {
	needs := ciNeeds(t)
	for _, tc := range []struct{ later, earlier string }{
		{"runner-manifests", "runner-images"},
		{"images", "runner-manifests"},
		{"images", "runner-images"},
		{"dev-binaries", "images"},
		{"dev-binaries", "runner-manifests"},
	} {
		if !reaches(needs, tc.later, tc.earlier) {
			t.Errorf("ci.yml's %s does not wait for %s, so the dev channel is not published in the order that keeps a host from pulling a runner image that is about to be replaced", tc.later, tc.earlier)
		}
	}
	// The other way round would be a cycle GitHub refuses, or the old order.
	for _, tc := range []struct{ earlier, later string }{
		{"runner-images", "dev-binaries"},
		{"runner-images", "images"},
		{"runner-manifests", "images"},
	} {
		if reaches(needs, tc.earlier, tc.later) {
			t.Errorf("ci.yml's %s waits for %s, which is the order the dev channel moved away from", tc.earlier, tc.later)
		}
	}
}

// A controller on a build from main sends its hosts to the dev channel, which
// cannot be asked for a commit, so the controller holds them until the release's
// notes name its build. That line is the word that the build is complete, so it
// is written after every file is up and verified, and an existing release keeps
// the line it has until then: a line written first would name a build whose
// binaries are still being replaced.
func TestTheDevReleaseNamesItsBuildOnlyAfterEveryFileIsUp(t *testing.T) {
	var wf struct {
		Jobs map[string]struct {
			Steps []struct {
				Name string            `yaml:"name"`
				Env  map[string]string `yaml:"env"`
				Run  string            `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(workflowFiles(t)["ci.yml"]), &wf); err != nil {
		t.Fatalf("parsing ci.yml: %v", err)
	}
	var run string
	var env map[string]string
	for _, step := range wf.Jobs["dev-binaries"].Steps {
		if step.Name == "Publish the dev release" {
			run, env = step.Run, step.Env
		}
	}
	if run == "" {
		t.Fatal("ci.yml's dev-binaries has no step named Publish the dev release")
	}
	if got := env["BUILD"]; got != "${{ steps.build.outputs.version }}" {
		t.Errorf("BUILD = %q, want the version the binaries were stamped with", got)
	}

	line := updates.ChannelBuildPrefix + "%s"
	named := strings.Index(run, line)
	verified := strings.Index(run, "the dev release is missing")
	if named < 0 {
		t.Fatalf("the publish step never writes a %q line into the notes, so the controller never learns the channel holds its build", line)
	}
	if verified < 0 || named < verified {
		t.Errorf("the notes name the build before the files are verified, so a controller could send hosts to a channel still being replaced")
	}
	if got := ciNotesNaming(run); got != 1 {
		t.Errorf("%d commands write the notes, want 1: the early edit of an existing release must leave them alone", got)
	}
	// What CI writes is what the controller reads.
	notes := fmt.Sprintf("Rolling development build.\n\n%s\n", line)
	notes = strings.Replace(notes, "%s", "main-sha-d49e8bf4", 1)
	if got := updates.ChannelBuild(notes); got != "main-sha-d49e8bf4" {
		t.Errorf("the controller reads %q from the notes CI writes, want main-sha-d49e8bf4", got)
	}
}

// ciNotesNaming counts the commands that edit an existing release's notes.
func ciNotesNaming(run string) int {
	n := 0
	for _, l := range strings.Split(run, "\n") {
		if strings.Contains(l, "gh release edit") && strings.Contains(l, "--notes") {
			n++
		}
	}
	return n
}
