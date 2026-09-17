package docs_test

import (
	"os"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/config"
)

// The validator judges scheduler.provision_timeout against how long a runner
// may take to start, and part of that is the wait the runner image does for its
// Docker daemon when runners.docker_wait leaves the choice to it. That default
// is written in the entrypoint, which is shell and cannot be imported, so
// config.ImageDockerWait restates it -- and a restated number drifts unless
// something reads both.
//
// Drift here is quiet in exactly the wrong way: the script would wait longer
// than the controller believed, so the controller would go on failing runners
// that were doing what the image told them to.
func TestTheImagesDockerWaitIsWhatTheValidatorBelieves(t *testing.T) {
	const script = "../../deploy/runner-entrypoint.sh"
	b, err := os.ReadFile(script)
	if err != nil {
		t.Fatalf("reading %s: %v", script, err)
	}
	m := regexp.MustCompile(`\$\{ZOOMIES_DOCKER_WAIT:-(\d+)\}`).FindSubmatch(b)
	if m == nil {
		t.Fatalf("%s no longer gives ZOOMIES_DOCKER_WAIT a default the way this test reads it; the two numbers cannot be held together by anything else", script)
	}
	secs, err := strconv.Atoi(string(m[1]))
	if err != nil {
		t.Fatalf("the default in %s is %q, which is not a number of seconds", script, m[1])
	}
	if got := time.Duration(secs) * time.Second; got != config.ImageDockerWait {
		t.Errorf("%s waits %s for the docker daemon but config.ImageDockerWait is %s; the provision timeout would be judged against a wait the image does not do",
			script, got, config.ImageDockerWait)
	}
}
