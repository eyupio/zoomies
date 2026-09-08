package drill

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"
)

// The drills' waits must fit inside the timeout the Makefile gives them, for
// the same reason the end-to-end harness's must: a drill killed by `go test`
// is reported as a timeout, which names nothing, instead of as the thing it
// was actually stuck on.
//
// No build tag, so it runs on every ordinary `go test ./...` rather than only
// where somebody has built the binary and asked for the drill tier.
func TestTheDrillsFitInsideTheMakefilesTimeout(t *testing.T) {
	root := repoRootFrom(t)
	raw, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatalf("reading the Makefile: %v", err)
	}
	re := regexp.MustCompile(`(?m)^\s+.*-tags drill.*-timeout (\S+)`)
	matches := re.FindAllStringSubmatch(string(raw), -1)
	if len(matches) == 0 {
		t.Fatal("no `go test -tags drill ... -timeout` line found in the Makefile; " +
			"if the target was renamed, update this test rather than deleting it")
	}
	for _, m := range matches {
		limit, err := time.ParseDuration(m[1])
		if err != nil {
			t.Fatalf("the Makefile's drill timeout %q is not a duration: %v", m[1], err)
		}
		if limit <= drillBudget {
			t.Errorf("the Makefile allows %s but one drill's waits need %s", limit, drillBudget)
		}
	}
}

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
