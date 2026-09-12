package docs

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const workflowDir = "../../.github/workflows"

func workflowFiles(t *testing.T) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(workflowDir)
	if err != nil {
		t.Fatalf("reading the workflows: %v", err)
	}
	out := map[string]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yml") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(workflowDir, e.Name()))
		if err != nil {
			t.Fatalf("reading %s: %v", e.Name(), err)
		}
		out[e.Name()] = string(b)
	}
	if len(out) == 0 {
		t.Fatal("no workflows found; this test is looking in the wrong place")
	}
	return out
}

// A tag is a name somebody else can move. Pinning to a commit is what makes a
// third-party action's code the code that was reviewed, and the version in the
// comment is what makes the pin legible and lets Dependabot move both together.
//
// CLAUDE.md has said this is enforced for some time and nothing enforced it.
var (
	usesLine = regexp.MustCompile(`(?m)^\s*(?:-\s+)?uses:\s*(\S+)\s*(#.*)?$`)
	pinned   = regexp.MustCompile(`^[^@]+@[0-9a-f]{40}$`)
)

func TestEveryActionIsPinnedToACommitWithItsVersion(t *testing.T) {
	for name, body := range workflowFiles(t) {
		for _, m := range usesLine.FindAllStringSubmatch(body, -1) {
			ref, comment := m[1], strings.TrimSpace(m[2])
			// A local action -- ./.github/actions/... -- is this repository's
			// own code and is already pinned by being in the commit.
			if strings.HasPrefix(ref, "./") {
				continue
			}
			if !pinned.MatchString(ref) {
				t.Errorf("%s: %q is not pinned to a commit; a tag is a name somebody else can move", name, ref)
				continue
			}
			if comment == "" {
				t.Errorf("%s: %q has no version comment, so nobody can tell what it is pinned to", name, ref)
			}
		}
	}
}

// A workflow that grants a permission at the top grants it to every job in it.
// The release workflow publishes a release from one job and images from
// others, and giving all of them both is how a build job ends up able to
// replace an artefact it has no business touching.
//
// A workflow with one job is left alone: there, the top of the file *is* the
// job, and demanding the same words a level down would be ceremony.
var jobLine = regexp.MustCompile(`(?m)^  [A-Za-z][A-Za-z0-9_-]*:$`)

func TestNoMultiJobWorkflowGrantsWriteToEveryJob(t *testing.T) {
	for name, body := range workflowFiles(t) {
		head, jobs, found := strings.Cut(body, "\njobs:")
		if !found {
			t.Errorf("%s has no jobs block", name)
			continue
		}
		if len(jobLine.FindAllString(jobs, -1)) < 2 {
			continue
		}
		perms, _, ok := strings.Cut(head, "\nenv:")
		if !ok {
			perms = head
		}
		idx := strings.Index(perms, "\npermissions:")
		if idx < 0 {
			continue
		}
		for _, line := range strings.Split(perms[idx:], "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasSuffix(line, ": write") {
				continue
			}
			t.Errorf("%s has more than one job and grants %q to all of them; move it to the job that needs it", name, line)
		}
	}
}

// The release workflow's own rules, each of which exists because of something
// that can turn a release tag into a lie about the bytes it names.
func TestTheReleaseWorkflowGuardsItsTag(t *testing.T) {
	body := workflowFiles(t)["release.yml"]
	if body == "" {
		t.Fatal("release.yml is missing")
	}
	for _, want := range []struct {
		needle, why string
	}{
		{"workflow_dispatch:", "a release must be startable by hand when the tag's own run cannot be"},
		{"prerelease:", "a tag with a hyphen is this project saying the release is not finished"},
		{"Refuse to rebuild a published release", "a full release tag must stay immutable once people can download it"},
		{"attest-build-provenance", "a checksum says the bytes match; provenance says where they came from"},
	} {
		if !strings.Contains(body, want.needle) {
			t.Errorf("release.yml has no %q: %s", want.needle, want.why)
		}
	}
}

func TestTheReleaseWorkflowStartsFromEachPublishedRelease(t *testing.T) {
	body := workflowFiles(t)["release.yml"]
	if body == "" {
		t.Fatal("release.yml is missing")
	}
	for _, want := range []string{
		"release:\n    types: [published]",
		"github.event.release.tag_name || inputs.tag",
		`draft: ${{ github.event_name == 'workflow_dispatch' }}`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("release.yml is missing %q, so creating a GitHub release may not build and attach its artifacts", want)
		}
	}
	if strings.Contains(body, "\n  push:\n") {
		t.Error("release.yml still listens for tag pushes as well as release publication, so creating a release can start two competing builds")
	}
}

func TestTheReleaseWorkflowOnlyLocksManualRebuildsOfPublishedFullReleases(t *testing.T) {
	body := workflowFiles(t)["release.yml"]
	if body == "" {
		t.Fatal("release.yml is missing")
	}
	for _, want := range []string{
		"--json isDraft,isPrerelease",
		`elif .isPrerelease then "prerelease"`,
		`EVENT: ${{ github.event_name }}`,
		`if [ "$EVENT" = "release" ]; then`,
		`if [ "$PRERELEASE" = "true" ]; then`,
		`else`,
		`::error::$TAG is already published.`,
		`exit 1`,
		`prerelease) echo "$TAG is already published as a prerelease; it will be replaced." ;;`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("release.yml is missing %q, so a published prerelease is still treated as final", want)
		}
	}
}

// Both images say which release they are. The runner images carried no version
// at all, which made them the images an operator is most likely to be holding
// when something is wrong and least able to identify.
func TestEveryPublishedImageSaysWhatItIs(t *testing.T) {
	// Per published target, not per file, and following FROM.
	//
	// Checking that the file mentioned each label was enough to pass while
	// deploy/Dockerfile's `agent` target carried none at all: the controller's
	// labels satisfied every search. That went unnoticed because nothing
	// published the agent image, and stopped being harmless the moment
	// something did.
	//
	// Which targets are published is not a list kept here -- it is whatever the
	// workflows name, so a new image is covered on the day it is added.
	// Inheritance counts: Dockerfile.runner's `runner` declares no labels and
	// needs none, because it is FROM runner-base, which does.
	required := []string{
		"org.opencontainers.image.source",
		"org.opencontainers.image.version",
		"org.opencontainers.image.revision",
		"org.opencontainers.image.created",
		"org.opencontainers.image.licenses",
	}

	published := map[string]bool{}
	for name, body := range workflowFiles(t) {
		for _, m := range regexp.MustCompile(`(?m)^\s*target:\s*(\S+)\s*$`).FindAllStringSubmatch(body, -1) {
			published[m[1]] = true
		}
		_ = name
	}
	if len(published) == 0 {
		t.Fatal("no workflow names a build target; this test is looking at the wrong thing")
	}

	// Every target in every Dockerfile: what it is FROM, and the text of its
	// own section.
	type stage struct{ from, body string }
	stages := map[string]stage{}
	for _, file := range []string{"../../deploy/Dockerfile", "../../deploy/Dockerfile.runner"} {
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("reading %s: %v", file, err)
		}
		body := string(b)
		marks := regexp.MustCompile(`(?m)^FROM\s+(\S+)(?:\s+AS\s+(\S+))?\s*$`).FindAllStringSubmatchIndex(body, -1)
		for i, m := range marks {
			if m[4] < 0 {
				continue // an unnamed stage cannot be a build target
			}
			end := len(body)
			if i+1 < len(marks) {
				end = marks[i+1][0]
			}
			stages[body[m[4]:m[5]]] = stage{from: body[m[2]:m[3]], body: body[m[0]:end]}
		}
	}

	// labelled walks a target and everything it is FROM, since a label on a
	// base image is on the images built from it.
	labelled := func(name, label string) bool {
		for seen := map[string]bool{}; ; {
			st, ok := stages[name]
			if !ok || seen[name] {
				return false
			}
			if strings.Contains(st.body, label) {
				return true
			}
			seen[name] = true
			name = st.from
		}
	}

	var checked int
	for name := range published {
		if _, ok := stages[name]; !ok {
			t.Errorf("a workflow builds target %q, which no Dockerfile defines", name)
			continue
		}
		checked++
		for _, label := range required {
			if !labelled(name, label) {
				t.Errorf("the %s image has no %s label, so a registry listing says nothing about what "+
					"it is and `docker inspect` cannot answer which build is running", name, label)
			}
		}
		// A label written from an ARG that is out of scope after FROM ships
		// silently empty, which reads as "no version" rather than as a mistake.
		if st := stages[name]; strings.Contains(st.body, `image.version="${VERSION}"`) && !strings.Contains(st.body, "ARG VERSION") {
			t.Errorf("the %s image labels a version from an ARG it never re-declares after FROM, "+
				"so the label ships empty", name)
		}
	}
	if checked == 0 {
		t.Fatal("no published target was checked")
	}
}

// :latest belongs to the release workflow, and to nothing else.
//
// Both workflows used to write it. Whichever ran last won, so a merge to main
// overwrote the :latest a release had just published and an operator who pulled
// it got an unreleased build stamped main-sha-abc1234. That is worse than it
// sounds: an agent is installed from a release asset and no release carries a
// main- version, so a controller on that image cannot match any agent it
// enrols, and shows every host as a different build for as long as it runs.
//
// main publishes :dev instead, which says what it is. This asserts the split
// both ways, because either half alone is the same bug wearing a new name.
func TestOnlyTheReleaseWorkflowPublishesLatest(t *testing.T) {
	files := workflowFiles(t)

	ci, ok := files["ci.yml"]
	if !ok {
		t.Fatal("no ci.yml")
	}
	release, ok := files["release.yml"]
	if !ok {
		t.Fatal("no release.yml")
	}

	// The two workflows spell an image name differently -- ci.yml through an
	// env var, release.yml in full -- so each is asserted the way it is
	// actually written. Checking release.yml's spelling against ci.yml is a
	// check that can never fire, which is what the first draft of this test
	// did.
	for _, img := range []struct{ ref, name string }{
		{"CONTROLLER_IMAGE", "ghcr.io/eyupio/zoomies"},
		{"AGENT_IMAGE", "ghcr.io/eyupio/zoomies-agent"},
	} {
		if strings.Contains(ci, "env."+img.ref+" }}:latest") {
			t.Errorf("ci.yml publishes %s:latest; that tag is the release workflow's, and a merge to main "+
				"would overwrite the release an operator asked for", img.name)
		}
		// main has to publish something, or its builds are reachable only by
		// commit.
		if !strings.Contains(ci, "env."+img.ref+" }}:dev") {
			t.Errorf("ci.yml does not publish %s:dev", img.name)
		}
		if !strings.Contains(release, img.name+":latest") {
			t.Errorf("release.yml no longer publishes %s:latest, so nothing does and the tag goes stale", img.name)
		}
		if !strings.Contains(release, img.name+":$REF") {
			t.Errorf("release.yml does not publish %s under its own tag", img.name)
		}
	}

	// The env vars the assertions above are written in terms of have to be the
	// images they are believed to be.
	for _, want := range []string{
		"CONTROLLER_IMAGE: ghcr.io/eyupio/zoomies\n",
		"AGENT_IMAGE: ghcr.io/eyupio/zoomies-agent\n",
	} {
		if !strings.Contains(ci, want) {
			t.Errorf("ci.yml does not define %q, so the checks above are testing a name nothing uses", strings.TrimSuffix(want, "\n"))
		}
	}

	// Runner tags are emitted from one loop for both repositories. They obey the
	// same contract: dev is main, latest is a full release.
	if strings.Contains(ci, `echo "$image:latest"`) {
		t.Error("ci.yml publishes a runner :latest tag; that tag is the release workflow's")
	}
	if !strings.Contains(ci, `echo "$image:dev"`) {
		t.Error("ci.yml does not publish the default runner variant under :dev")
	}
	if !strings.Contains(release, `echo "ghcr.io/eyupio/$image:latest"`) {
		t.Error("release.yml no longer publishes runner :latest tags")
	}
	for _, want := range []string{
		"RUNNER_IMAGE: ghcr.io/eyupio/zoomies-runner\n",
		"RUNNER_DOCKER_IMAGE: ghcr.io/eyupio/zoomies-runner-docker\n",
	} {
		if !strings.Contains(ci, want) {
			t.Errorf("ci.yml does not define %q", strings.TrimSuffix(want, "\n"))
		}
	}

	// And the gate that keeps a prerelease from becoming what :latest means.
	if !strings.Contains(release, `if [ "$PRERELEASE" != "true" ]; then`) {
		t.Error("release.yml no longer gates :latest on the release not being a prerelease")
	}
}

func TestMainPublishesAnInstallableDevBinary(t *testing.T) {
	ci := workflowFiles(t)["ci.yml"]
	for _, want := range []string{
		"dev-binaries:",
		"github.event_name == 'push' && github.ref == 'refs/heads/main'",
		"make dist VERSION=${{ steps.build.outputs.version }}",
		"tag_name: dev",
		"prerelease: true",
		"make_latest: false",
		"overwrite_files: true",
	} {
		if !strings.Contains(ci, want) {
			t.Errorf("ci.yml is missing %q, so --version dev is not a complete rolling binary channel", want)
		}
	}
}

func TestMainPublishingRunsCannotCancelOneAnother(t *testing.T) {
	ci := workflowFiles(t)["ci.yml"]
	want := "cancel-in-progress: ${{ github.ref != 'refs/heads/main' }}"
	if !strings.Contains(ci, want) {
		t.Fatalf("ci.yml is missing %q; a newer main run can cancel the publisher and leave :dev stale", want)
	}
}

// An agent host that wants a container needs an image that is an agent.
//
// deploy/Dockerfile has carried an `agent` target since it was written and no
// workflow named it, so the only way to run one was to run the controller image
// and override its command.
func TestTheAgentImageIsBuiltAndPublished(t *testing.T) {
	files := workflowFiles(t)
	for _, name := range []string{"ci.yml", "release.yml"} {
		body, ok := files[name]
		if !ok {
			t.Fatalf("no %s", name)
		}
		if !strings.Contains(body, "target: agent") {
			t.Errorf("%s builds no `agent` target, so deploy/Dockerfile's agent image is published by nothing", name)
		}
	}
}

func TestCIDogfoodsZoomiesWithRecoveryForEveryJob(t *testing.T) {
	files := workflowFiles(t)
	selector := `((github.event_name == 'workflow_dispatch' && inputs.runner == 'github') || (github.event_name == 'pull_request' && github.event.pull_request.head.repo.fork)) && 'ubuntu-latest' || 'zoomies-linux-x64'`
	runsOn := regexp.MustCompile(`(?m)^    runs-on: (.+)$`)
	for _, name := range []string{"ci.yml", "govulncheck.yml"} {
		body := files[name]
		for _, want := range []string{"default: zoomies", "options: [zoomies, github]"} {
			if !strings.Contains(body, want) {
				t.Errorf("%s lacks %s", name, want)
			}
		}
		matches := runsOn.FindAllStringSubmatch(body, -1)
		if len(matches) == 0 {
			t.Fatalf("%s has no job runners", name)
		}
		// Match exceptions by job ID: a count alone lets the wrong job stop
		// dogfooding while still satisfying the test. The first two need
		// system packages unavailable to the unprivileged stock runner; the
		// other two exist to run on a platform the Zoomies pool has no host
		// for, which is the whole point of each.
		exceptions := map[string]string{}
		found := map[string]bool{}
		if name == "ci.yml" {
			exceptions["installer"] = "ubuntu-latest"
			exceptions["playwright"] = "ubuntu-latest"
			exceptions["arm64"] = "ubuntu-24.04-arm"
			exceptions["windows"] = "windows-latest"
		}
		jobs := regexp.MustCompile(`(?m)^  ([a-zA-Z0-9_-]+):\n`).FindAllStringSubmatchIndex(body, -1)
		for i, job := range jobs {
			end := len(body)
			if i+1 < len(jobs) {
				end = jobs[i+1][0]
			}
			id := body[job[2]:job[3]]
			runner := runsOn.FindStringSubmatch(body[job[1]:end])
			if len(runner) == 0 {
				continue
			}
			if want, allowed := exceptions[id]; allowed {
				found[id] = true
				if runner[1] != want {
					t.Errorf("%s job %s must run on the GitHub-hosted %s, got %s", name, id, want, runner[1])
				}
			} else if !strings.Contains(runner[1], selector) {
				t.Errorf("%s job %s lacks fork protection and manual recovery: %s", name, id, runner[1])
			}
		}
		for id := range exceptions {
			if !found[id] {
				t.Errorf("%s is missing expected GitHub-hosted job %s", name, id)
			}
		}
	}
	ci := files["ci.yml"]
	if strings.Contains(ci, "useblacksmith/") {
		t.Error("CI still depends on Blacksmith-specific builders")
	}
	if !strings.Contains(ci, "if: ${{ !cancelled() && (github.event_name == 'pull_request'") {
		t.Error("Images must respect cancellation while allowing skipped PR publishing dependencies")
	}
}

func TestContainerBuildsStampTheirRunnerChannel(t *testing.T) {
	workflows := workflowFiles(t)
	for _, tc := range []struct {
		file, stamp string
		count       int
	}{
		{"ci.yml", "RUNNER_IMAGE_TAG=dev", 2},
		{"release.yml", "RUNNER_IMAGE_TAG=${{ needs.setup.outputs.tag }}", 4},
	} {
		if got := strings.Count(workflows[tc.file], tc.stamp); got != tc.count {
			t.Errorf("%s: found %d runner tag stamps, want %d controller/agent build paths", tc.file, got, tc.count)
		}
	}
	dockerfile, err := os.ReadFile("../../deploy/Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(dockerfile), "-X github.com/eyupio/zoomies/internal/naming.RunnerImageTag=${RUNNER_IMAGE_TAG}") {
		t.Error("container build does not pass its runner tag to the binary")
	}
}
