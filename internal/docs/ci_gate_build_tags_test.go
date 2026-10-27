package docs

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// `go vet ./...` does not compile a file behind a build tag, so a suite behind
// one can stop building and every gate stays green. test/e2e did exactly that:
// a non-test file referred to a symbol only its _test.go half defined. The
// Makefile's `vet-tags` target vets each tagged suite, `make lint` and CI both
// run it, and this holds the target to the tags the repository actually uses,
// so a new suite behind a new tag cannot be added and forgotten.

// Tags that do not name a suite: the platforms, and `ignore`, which marks a
// `go run` generator. The rest each need a line in vet-tags, or a reason here.
var tagsThatNeedNoVet = map[string]string{
	"unix": "a platform", "windows": "a platform", "linux": "a platform", "darwin": "a platform",
	"ignore": "a generator run with `go run`, not part of any package",
	"helpercheck": "compiled and run by test/upgrade/helper-check.sh, which the upgrade job " +
		"runs through `make test-upgrade`",
}

func TestEveryBuildTagIsVettedByTheVetTagsTarget(t *testing.T) {
	makefile, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatalf("reading the Makefile: %v", err)
	}
	recipe := between(string(makefile), "\nvet-tags:", "\n\n")
	if recipe == "" {
		t.Fatal("the Makefile has no vet-tags target, so the tagged suites are never compiled by a check")
	}
	vetted := map[string]bool{}
	for _, m := range regexp.MustCompile(`vet -tags ([A-Za-z0-9_,]+)`).FindAllStringSubmatch(recipe, -1) {
		for _, tag := range strings.Split(m[1], ",") {
			vetted[tag] = true
		}
	}

	used := map[string]string{} // tag -> a file that uses it
	err = filepath.WalkDir("../..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "webdist", "dist", "site":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		for _, tag := range buildTags(t, path) {
			if used[tag] == "" {
				used[tag] = strings.TrimPrefix(filepath.ToSlash(path), "../../")
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(used) == 0 {
		t.Fatal("no build tags were found anywhere; the test is not reading the tree")
	}

	var unvetted []string
	for tag, file := range used {
		if vetted[tag] {
			continue
		}
		if _, ok := tagsThatNeedNoVet[tag]; ok {
			continue
		}
		unvetted = append(unvetted, tag+" (first used in "+file+")")
	}
	sort.Strings(unvetted)
	if len(unvetted) > 0 {
		t.Errorf("these build tags are used and no `go vet -tags` line in the Makefile's vet-tags target covers them; add the line, or list the tag in this test with the reason it needs none:\n  %s",
			strings.Join(unvetted, "\n  "))
	}
	for tag := range vetted {
		if used[tag] == "" {
			t.Errorf("vet-tags vets -tags %s, which no file uses; remove the line", tag)
		}
	}
}

// The vet-tags step used to sit in the `rest` shard, in front of its tests,
// which made that shard the merge path: 8 to 11 minutes of whole-tree work
// before the shard that gates the merge ran anything of its own. It lives in
// `vet` now, beside the shards. What this holds is that exactly one job runs it,
// unconditionally, and that ci-gate waits on that job, because a step in a job
// outside the gate can run green or red without deciding a merge.
func TestCIRunsTheVetTagsTarget(t *testing.T) {
	raw, err := os.ReadFile("../../.github/workflows/ci.yml")
	if err != nil {
		t.Fatalf("reading ci.yml: %v", err)
	}
	var workflow struct {
		Jobs map[string]struct {
			Needs jobNeeds `yaml:"needs"`
			Steps []struct {
				Run             string `yaml:"run"`
				If              string `yaml:"if"`
				ContinueOnError any    `yaml:"continue-on-error"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatalf("ci.yml does not parse: %v", err)
	}
	var runners []string
	for id, job := range workflow.Jobs {
		for _, step := range job.Steps {
			if strings.TrimSpace(step.Run) != "make vet-tags" {
				continue
			}
			runners = append(runners, id)
			if step.If != "" {
				t.Errorf("job %s runs the vet-tags step conditionally (%q), so a change it is meant to catch can skip it", id, step.If)
			}
			if step.ContinueOnError != nil {
				t.Errorf("job %s's vet-tags step sets continue-on-error (%v), which makes it unable to fail the job", id, step.ContinueOnError)
			}
		}
	}
	sort.Strings(runners)
	switch len(runners) {
	case 0:
		t.Fatal("no job in ci.yml runs `make vet-tags`, so a suite behind a build tag can stop compiling with every gate green")
	case 1:
	default:
		t.Fatalf("these jobs run `make vet-tags`: %v; it compiles every tagged suite once and belongs in one job, which then gates the merge", runners)
	}
	if gate, ok := workflow.Jobs["ci-gate"]; ok && !slices.Contains([]string(gate.Needs), runners[0]) {
		t.Errorf("the vet-tags check runs in job %s and ci-gate does not wait on it, so it can fail without blocking a merge", runners[0])
	}
}

// buildTags returns the identifiers in a file's //go:build line, if it has one
// before its package clause.
func buildTags(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var tags []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "package ") {
			break
		}
		expr, ok := strings.CutPrefix(line, "//go:build ")
		if !ok {
			continue
		}
		tags = append(tags, buildTagWord.FindAllString(expr, -1)...)
	}
	return tags
}

// One identifier in a build constraint, between the operators and parentheses.
var buildTagWord = regexp.MustCompile(`[A-Za-z0-9_.]+`)
