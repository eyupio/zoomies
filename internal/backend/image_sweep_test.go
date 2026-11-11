package backend

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

const runnerRepo = "ghcr.io/eyupio/zoomies-runner-docker"

// sweepFixture is a host that has prepared the runner image and holds, beside
// it, every kind of untagged image the sweep has to tell apart.
func sweepFixture(t *testing.T) (*DockerBackend, *[]string, *[]string) {
	t.Helper()
	untagged := func(id string, digests ...string) ImageSummary {
		return ImageSummary{ID: id, RepoTags: []string{"<none>:<none>"}, RepoDigests: digests}
	}
	images := []ImageSummary{
		untagged("sha256:superseded", runnerRepo+"@sha256:aaa"),
		untagged("sha256:in-use", runnerRepo+"@sha256:bbb"),
		untagged("sha256:just-handed", runnerRepo+"@sha256:ccc"),
		untagged("sha256:strangers", "coollabsio/sentinel@sha256:ddd"),
		untagged("sha256:built-here"),
		untagged("sha256:two-origins", runnerRepo+"@sha256:eee", "coollabsio/sentinel@sha256:eee"),
		{ID: "sha256:tagged", RepoTags: []string{runnerRepo + ":1.2.3"}, RepoDigests: []string{runnerRepo + "@sha256:fff"}},
		untagged("sha256:also-superseded", runnerRepo+"@sha256:ggg"),
	}
	var deleted, queries []string
	f := newFakeEngine(t, map[string]http.HandlerFunc{
		"GET " + v + "/images/json": func(w http.ResponseWriter, r *http.Request) {
			if !strings.Contains(r.URL.Query().Get("filters"), `"dangling":["true"]`) {
				t.Errorf("image list filters = %q; without the dangling filter the sweep would list images a tag still names", r.URL.Query().Get("filters"))
			}
			writeJSON(w, http.StatusOK, images)
		},
		"DELETE " + v + "/images/{id...}": func(w http.ResponseWriter, r *http.Request) {
			id := r.PathValue("id")
			queries = append(queries, r.URL.RawQuery)
			if id == "sha256:in-use" {
				writeJSON(w, http.StatusConflict, map[string]string{"message": "conflict: image is being used by a container"})
				return
			}
			deleted = append(deleted, id)
			writeJSON(w, http.StatusOK, []map[string]string{{"Deleted": id}})
		},
	})
	b := dockerBackendFor(t, f, DockerOptions{})
	b.imagesSince = time.Now().Add(-time.Hour)
	// The tag moved through each of these digests before it reached the current
	// one, which is the history that makes them superseded.
	for _, d := range []string{"sha256:aaa", "sha256:bbb", "sha256:ccc", "sha256:eee", "sha256:ggg", "sha256:current"} {
		b.noteImage(runnerRepo+":dev", d)
	}
	clear(b.images.handed)
	b.images.handed["sha256:ccc"] = time.Now()
	return b, &deleted, &queries
}

// An untagged image of a repository the agent prepared a runner from is what
// an image update leaves behind. The sweep removes it, and only it: not a tagged
// image, not a stranger's, not one built on the host, not one a create has just
// been handed, and not one a container uses.
func TestTheImageSweepRemovesOnlyTheSupersededImagesOfRepositoriesItPulled(t *testing.T) {
	b, deleted, queries := sweepFixture(t)

	removed, err := b.SweepImages(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"sha256:superseded", "sha256:also-superseded"}
	if removed != len(want) || !slices.Equal(*deleted, want) {
		t.Fatalf("sweep removed %d: %v; want exactly the untagged images of the runner repository that nothing uses: %v (%s)", removed, *deleted, want, agentOwnsPromise)
	}
	for _, q := range *queries {
		if !strings.Contains(q, "noprune=true") || strings.Contains(q, "force") {
			t.Errorf("image delete query = %q; it must not force a removal or prune parents, because the daemon's refusal of an image a container uses is the guard", q)
		}
	}
}

// A create resolves a reference to a digest and then starts a container from
// it. An image removed between the two fails the create, so a digest handed out
// recently is left alone however stale its tag is.
func TestTheImageSweepLeavesAnImageARunnerWasJustHanded(t *testing.T) {
	b, deleted, _ := sweepFixture(t)
	b.images.handed["sha256:aaa"] = time.Now().Add(-imageHandoff / 2)

	if _, err := b.SweepImages(context.Background()); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(*deleted, "sha256:superseded") {
		t.Fatalf("sweep removed an image handed to a runner %s ago; a create in flight would fail", imageHandoff/2)
	}

	b.images.handed["sha256:aaa"] = time.Now().Add(-2 * imageHandoff)
	if _, err := b.SweepImages(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(*deleted, "sha256:superseded") {
		t.Fatal("sweep still left the image after the handoff passed; it would never be cleaned")
	}
}

// A restarted agent knows nothing of the creates that were under way when it
// stopped, and nothing of which repositories are its own until it prepares an
// image. Until then it removes nothing.
func TestTheImageSweepRemovesNothingUntilTheAgentHasSettledAndPreparedAnImage(t *testing.T) {
	b, _, _ := sweepFixture(t)
	b.imagesSince = time.Now().Add(-imageHandoff / 2)
	if removed, err := b.SweepImages(context.Background()); err != nil || removed != 0 {
		t.Fatalf("a young agent swept %d images (err %v); want none until %s have passed", removed, err, imageHandoff)
	}

	empty := newFakeEngine(t, map[string]http.HandlerFunc{})
	fresh := dockerBackendFor(t, empty, DockerOptions{})
	fresh.imagesSince = time.Now().Add(-time.Hour)
	if removed, err := fresh.SweepImages(context.Background()); err != nil || removed != 0 {
		t.Fatalf("an agent that prepared no image swept %d (err %v); every repository would be a stranger's", removed, err)
	}
	if len(empty.seen) != 0 {
		t.Fatalf("an agent that prepared no image still sent %d requests to the daemon", len(empty.seen))
	}
}

// The daemon's listing spells a Docker Hub repository without its registry or
// namespace, and an operator's pool may spell it either way.
func TestRepositoriesAreComparedInTheDaemonsSpelling(t *testing.T) {
	for ref, want := range map[string]string{
		"postgres:16":                               "postgres",
		"docker.io/library/postgres:16":             "postgres",
		"library/postgres":                          "postgres",
		"index.docker.io/library/postgres@sha256:a": "postgres",
		"docker.io/someone/runner:1":                "someone/runner",
		"GHCR.io/Eyupio/Zoomies-Runner:dev":         "ghcr.io/eyupio/zoomies-runner",
		"registry.internal:5000/team/runner:2":      "registry.internal:5000/team/runner",
	} {
		name, _ := splitImageRef(ref)
		if got := canonicalRepository(name); got != want {
			t.Errorf("canonicalRepository(%q) = %q, want %q", ref, got, want)
		}
	}
}

// An untagged image the agent never saw a tag resolve to has no history to be
// superseded from: it may be a stranger's build, or one pinned by digest. Nor is
// the digest a tag resolves to now ever superseded, however idle it is.
func TestTheImageSweepLeavesAnImageThatWasNeverSupersededByATag(t *testing.T) {
	b, deleted, _ := sweepFixture(t)
	b.noteImage(runnerRepo+"@sha256:pinned", "sha256:pinned")
	b.images.handed = map[string]time.Time{}
	b.images.current[runnerRepo+":stable"] = "sha256:bbb"

	f := newFakeEngine(t, map[string]http.HandlerFunc{
		"GET " + v + "/images/json": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, []ImageSummary{
				{ID: "sha256:never-seen", RepoTags: []string{"<none>:<none>"}, RepoDigests: []string{runnerRepo + "@sha256:zzz"}},
				{ID: "sha256:pinned-img", RepoTags: []string{"<none>:<none>"}, RepoDigests: []string{runnerRepo + "@sha256:pinned"}},
				{ID: "sha256:current-img", RepoTags: []string{"<none>:<none>"}, RepoDigests: []string{runnerRepo + "@sha256:bbb"}},
			})
		},
		"DELETE " + v + "/images/{id...}": func(w http.ResponseWriter, r *http.Request) {
			*deleted = append(*deleted, r.PathValue("id"))
			writeJSON(w, http.StatusOK, []map[string]string{})
		},
	})
	b.api = dockerBackendFor(t, f, DockerOptions{}).api
	*deleted = nil
	removed, err := b.SweepImages(context.Background())
	if err != nil || removed != 0 || len(*deleted) != 0 {
		t.Fatalf("sweep removed %d (%v, err %v); an image with no tag history, one pinned by digest and one a tag still names must all stay", removed, *deleted, err)
	}
}

// A digest handed out long ago protects nothing, so the ledger forgets it
// instead of growing for the life of the agent.
func TestTheImageLedgerForgetsDigestsOlderThanTheHandoff(t *testing.T) {
	b, _, _ := sweepFixture(t)
	b.images.handed["sha256:old"] = time.Now().Add(-2 * imageHandoff)
	b.noteImage(runnerRepo+":dev", "sha256:newer")
	if _, ok := b.images.handed["sha256:old"]; ok {
		t.Fatal("a digest handed out twice the handoff ago is still remembered")
	}
}
