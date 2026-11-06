package docs

import (
	"os/exec"
	"testing"
)

// The deploy tells search engines which pages changed only if it can read the
// sitemap that is live, and the site's edge refuses Python's default
// User-Agent. hooks/indexnow_test.py serves a sitemap that behaves the same way,
// so a request that names itself badly fails here and not silently in a deploy.
func TestIndexNowHookReadsTheLiveSitemap(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("Python is required to exercise the IndexNow hook")
	}
	cmd := exec.Command(python, "-I", "indexnow_test.py")
	cmd.Dir = "../../hooks"
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("IndexNow hook tests: %v\n%s", err, output)
	}
}
