package backend

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// These tests pin the agent to what docs/security.md ("What the agent owns on
// a host") promises a team before they run the enrolment command on their own
// machine. A prune that widens -- to images, to volumes, to a container
// somebody else started -- is a promise broken on a machine that is not ours,
// so each failure names the promise rather than the endpoint.

const agentOwnsPromise = `docs/security.md, "What the agent owns on a host"`

// The one daemon-wide thing the agent does is ask Docker to prune unused
// builder cache down to a target. Decision 25 keeps exactly that and nothing
// broader: no filter that reaches further, no second endpoint, and nothing at
// all when the target is zero, which is the shared-daemon advice.
func TestTheBuilderCachePruneAsksForNothingButUnusedCacheAboveTheTarget(t *testing.T) {
	f := newFakeEngine(t, map[string]http.HandlerFunc{
		"POST " + v + "/build/prune": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{"SpaceReclaimed": 42})
		},
	})
	b := dockerBackendFor(t, f, DockerOptions{})

	if _, err := b.PruneBuildCache(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if len(f.seen) != 0 {
		t.Fatalf("a builder-cache target of 0 sent %d requests; 0 is the shared-daemon advice and must prune nothing (%s)", len(f.seen), agentOwnsPromise)
	}

	freed, err := b.PruneBuildCache(context.Background(), 5<<30)
	if err != nil {
		t.Fatal(err)
	}
	if freed != 42 {
		t.Errorf("freed = %d, want what the daemon reported", freed)
	}
	if len(f.seen) != 1 {
		t.Fatalf("the prune sent %d requests, want exactly one to /build/prune (%s)", len(f.seen), agentOwnsPromise)
	}
	r := f.seen[0]
	if r.Method != http.MethodPost || r.URL.Path != v+"/build/prune" {
		t.Fatalf("the prune sent %s %s; the builder cache is the only thing the agent prunes daemon-wide (%s)", r.Method, r.URL.Path, agentOwnsPromise)
	}
	q := r.URL.Query()
	keys := slices.Sorted(func(yield func(string) bool) {
		for k := range q {
			if !yield(k) {
				return
			}
		}
	})
	if !slices.Equal(keys, []string{"all", "keep-storage"}) {
		t.Errorf("the prune sent parameters %v; anything beyond the target reaches further than decision 25 allows (%s)", keys, agentOwnsPromise)
	}
	if q.Get("keep-storage") != "5368709120" {
		t.Errorf("keep-storage = %q, want the configured target in bytes", q.Get("keep-storage"))
	}
}

// Podman has no builder cache for the agent to prune, and the promise says so.
func TestPodmanPrunesNoBuilderCache(t *testing.T) {
	f := newFakeEngine(t, nil)
	b, err := NewPodman(DockerOptions{Host: "tcp://" + f.Listener.Addr().String(), Logger: quietLogger()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.PruneBuildCache(context.Background(), 5<<30); err != nil {
		t.Fatal(err)
	}
	if len(f.seen) != 0 {
		t.Fatalf("a Podman backend sent %d requests to prune builder cache (%s)", len(f.seen), agentOwnsPromise)
	}
}

// The label is the whole boundary of which containers the agent touches: it
// lists, and therefore reaps, only what it labelled.
func TestTheAgentListsOnlyContainersCarryingItsLabel(t *testing.T) {
	f := newFakeEngine(t, map[string]http.HandlerFunc{
		"GET " + v + "/containers/json": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, []any{})
		},
	})
	if _, err := dockerBackendFor(t, f, DockerOptions{}).List(context.Background()); err != nil {
		t.Fatal(err)
	}
	r := f.request(http.MethodGet, v+"/containers/json")
	if r == nil {
		t.Fatal("List sent no container listing")
	}
	if !strings.Contains(r.URL.Query().Get("filters"), LabelManaged+"=true") {
		t.Fatalf("List filters = %q; without %s=true the agent would reap containers it did not start (%s)",
			r.URL.Query().Get("filters"), LabelManaged, agentOwnsPromise)
	}
}

// Nothing anywhere in the tree may ask a daemon to prune images, containers,
// volumes or networks, or delete an image or a volume. This reads the source
// rather than a fake daemon because the promise is about every code path,
// including one written next year that no fake here would think to exercise.
func TestNothingInTheTreePrunesImagesVolumesOrUnlabelledContainers(t *testing.T) {
	forbidden := []*regexp.Regexp{
		regexp.MustCompile(`"/(images|containers|volumes|networks)/prune"`),
		regexp.MustCompile(`MethodDelete,\s*"/(images|volumes)/`),
		regexp.MustCompile(`"DELETE",\s*"/(images|volumes)/`),
		regexp.MustCompile(`"(docker|podman)",\s*"(image|volume|system|container)",\s*"prune"`),
	}
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "node_modules", ".git", "web", "site", "dist":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, re := range forbidden {
			if m := re.Find(body); m != nil {
				t.Errorf("%s: %s -- the agent prunes only unused builder cache, and removes only containers it labelled (%s)", path, m, agentOwnsPromise)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
