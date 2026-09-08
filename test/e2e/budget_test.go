package e2e

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"
)

// The harness's waits must fit inside the timeout the Makefile gives it.
//
// They did not: the scenario asked for twenty-seven minutes of waiting behind
// `-timeout 20m`, so it could never have reached its own last assertion. A run
// that was going to fail on a leftover registration would have been reported
// as a timeout instead, which is the worst kind of red -- it names nothing.
//
// This test has no build tag, so it runs on every ordinary `go test ./...`
// rather than only when somebody has real GitHub credentials.
func TestTheScenarioFitsInsideTheMakefilesTimeout(t *testing.T) {
	root := repoRootFrom(t)
	raw, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatalf("reading the Makefile: %v", err)
	}

	// Every e2e target's -timeout, so adding one cannot quietly skip this.
	re := regexp.MustCompile(`(?m)^\s+\$\(GO\) test .*-tags e2e.*-timeout (\S+)`)
	matches := re.FindAllStringSubmatch(string(raw), -1)
	if len(matches) == 0 {
		t.Fatal("no `go test -tags e2e ... -timeout` line found in the Makefile; " +
			"if the target was renamed, update this test rather than deleting it")
	}
	for _, m := range matches {
		limit, err := time.ParseDuration(m[1])
		if err != nil {
			t.Fatalf("the Makefile's e2e timeout %q is not a duration: %v", m[1], err)
		}
		if limit <= scenarioBudget {
			t.Errorf("the Makefile allows %s but the scenario's waits and cleanup need %s; "+
				"the test would be killed before its own last assertion", limit, scenarioBudget)
		}
	}
}

// repoRootFrom walks up to the directory holding go.mod. It is duplicated from
// the tagged half of this package rather than shared, because this test has to
// compile without the e2e tag and that half does not.
func repoRootFrom(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for range 6 {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("could not find the repository root from the test's working directory")
	return ""
}
