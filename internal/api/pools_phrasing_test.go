package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/store"
)

// A digest pins an image to exactly one build, so accepting something that
// merely looks like one would pin a pool to nothing at all.
func TestDigestReferenceAcceptsOnlyARealOne(t *testing.T) {
	const sha = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	for _, ref := range []string{
		"ghcr.io/eyupio/zoomies-runner@sha256:" + sha,
		"runner@sha256:" + strings.ToUpper(sha),
	} {
		if !digestReference(ref) {
			t.Errorf("digestReference(%q) = false, want true", ref)
		}
	}

	for _, ref := range []string{
		"",
		"ghcr.io/eyupio/zoomies-runner:ubuntu-24.04", // a tag, not a digest
		"@sha256:" + sha,                           // nothing to pin
		"runner@sha256:" + sha[:63],                // a digest one character short
		"runner@sha256:" + sha + "0",               // and one too long
		"runner@sha256:" + strings.Repeat("z", 64), // not hexadecimal
		"runner@sha256:" + sha + "@sha256:" + sha,  // two of them
		"runner@sha512:" + sha,                     // a different algorithm
	} {
		if digestReference(ref) {
			t.Errorf("digestReference(%q) = true, want false", ref)
		}
	}
}

// A repository name reaches a cache directory, so anything that could climb out
// of one is refused before it is ever joined onto a path.
func TestValidRepositoryPathRefusesAnythingThatCouldClimbOut(t *testing.T) {
	for _, repo := range []string{"acme/widgets", "acme/widgets.git", "a/b"} {
		if !validRepositoryPath(repo) {
			t.Errorf("validRepositoryPath(%q) = false, want true", repo)
		}
	}
	for _, repo := range []string{
		"", "acme", "/widgets", "acme/", "acme/widgets/extra",
		"../widgets", "acme/..", "./widgets", "acme/.",
	} {
		if validRepositoryPath(repo) {
			t.Errorf("validRepositoryPath(%q) = true, want false", repo)
		}
	}
}

// The wizard's warning before creation and the scheduler's problem afterwards
// are the same fact, so they are kept in the same words -- and "no other
// backend either" has to be said plainly rather than left as an empty list.
func TestSwitchToOffersTheBackendsThereActuallyAre(t *testing.T) {
	if got := switchTo(nil); !strings.Contains(got, "no other backend") {
		t.Errorf("switchTo(nil) = %q, want it to say there are none", got)
	}
	one := switchTo([]string{"podman"})
	if !strings.Contains(one, "podman") || !strings.Contains(one, "already offer") {
		t.Errorf("switchTo(one) = %q", one)
	}
	many := switchTo([]string{"podman", "process"})
	if !strings.Contains(many, "podman, process") {
		t.Errorf("switchTo(many) = %q, want both listed", many)
	}
}

// Sentences an operator reads are written for a person, and "1 runners" is the
// sort of thing that makes a tool feel unfinished.
func TestRunnerCountReadsLikeASentence(t *testing.T) {
	for n, want := range map[int]string{0: "0 runners", 1: "1 runner", 2: "2 runners"} {
		if got := runnerCount(n); got != want {
			t.Errorf("runnerCount(%d) = %q, want %q", n, got, want)
		}
	}
}

// Prewarming pulls a pool's image on every host that could run it, which is
// only a thing a container backend can do at all -- so a pool that cannot is
// refused with a sentence rather than left to fail silently later.
func TestPrewarmingAPoolThatCannotBePrewarmed(t *testing.T) {
	h := newHarness(t)
	admin, _ := h.user("admin", store.RoleAdmin)
	inst := h.installation()

	// The bare-process backend has no image to pull.
	pool := &store.Pool{
		Name: "zoomies-process", InstallationID: inst.ID, Labels: store.StringSlice{"zoomies-process"},
		Backend: store.BackendProcess, MaxRunners: 2, DockerMode: store.DockerNone, Enabled: true,
	}
	if err := h.st.CreatePool(h.ctx, pool); err != nil {
		t.Fatalf("CreatePool: %v", err)
	}

	resp := h.do(request{method: http.MethodPost, path: "/api/v1/pools/" + pool.ID + "/prewarm",
		cookie: h.session(admin)})
	if resp.status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %s", resp.status, resp.body)
	}
	if resp.errorMessage(t) == "" {
		t.Fatalf("the refusal said nothing an operator can act on:\n%s", resp.body)
	}

	missing := h.do(request{method: http.MethodPost, path: "/api/v1/pools/pool_nope/prewarm",
		cookie: h.session(admin)})
	if missing.status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", missing.status, missing.body)
	}
}

// Prewarming a pool that can be prewarmed says how many hosts were asked, so
// an operator knows whether the request reached anything at all.
func TestPrewarmingADockerPoolSaysHowManyHostsWereAsked(t *testing.T) {
	h := newHarness(t)
	admin, _ := h.user("admin", store.RoleAdmin)
	inst := h.installation()
	pool := h.pool(inst, "zoomies-4vcpu")
	h.host("vm-1")

	resp := h.do(request{method: http.MethodPost, path: "/api/v1/pools/" + pool.ID + "/prewarm",
		cookie: h.session(admin)})
	// Accepted rather than OK: the pulls are queued for the hosts to do, and
	// the answer is what was asked for rather than what has happened.
	if resp.status != http.StatusAccepted {
		t.Fatalf("status = %d, want 202: %s", resp.status, resp.body)
	}
	body := resp.json(t)
	if body["queued"] != float64(1) {
		t.Fatalf("queued = %v, want the one host that could take it:\n%s", body["queued"], resp.body)
	}
}
