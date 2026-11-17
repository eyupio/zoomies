package controller

import (
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/agent"
	"github.com/eyupio/zoomies/internal/backend"
	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/registrymirror"
	"github.com/eyupio/zoomies/internal/store"
)

// A host's agent says what is true of its registry mirror and the controller
// says so to the operator, but only where it costs something: a mirror that is
// broken matters to the pools that would have used it, and to nobody else. The
// agent's words are a fixed set and every sentence shown is the controller's,
// because what an agent sends is not trusted to be rendered as it arrives.

func (h *harness) dindPool(inst *store.Installation, name string, mutate func(*store.Pool)) *store.Pool {
	h.t.Helper()
	p := h.pool(inst, name)
	p.DockerMode = store.DockerDinD
	if mutate != nil {
		mutate(p)
	}
	if err := h.st.UpdatePool(h.ctx, p); err != nil {
		h.t.Fatalf("UpdatePool: %v", err)
	}
	return p
}

func (h *harness) mirrorBeat(hostID string, rep *backend.RegistryMirrorReport) {
	h.t.Helper()
	if _, err := h.c.Heartbeat(h.ctx, hostID, agent.HeartbeatRequest{RegistryMirror: rep}); err != nil {
		h.t.Fatalf("Heartbeat: %v", err)
	}
}

func TestABrokenMirrorIsAProblemOnlyForAPoolThatWouldHaveUsedIt(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	host := h.hostWith("builder")
	h.mirrorBeat(host.ID, &backend.RegistryMirrorReport{Problem: backend.MirrorProblemListener})
	if hasCode(h.problemCodes(), "host.registry_mirror_degraded") {
		t.Fatal("raised with no pool that runs a docker-in-docker sidecar")
	}

	h.dindPool(inst, "dind", nil)
	got := h.problem(t, "host.registry_mirror_degraded")
	if got.TargetKind != "host" || got.TargetID != host.ID || got.Severity != config.SeverityWarning {
		t.Errorf("problem = %+v, want a warning on the host", got)
	}
	if got.Detail == "" || got.Fix == "" {
		t.Errorf("problem = %+v, want it to say what is wrong and what to do", got)
	}
	if !strings.Contains(got.Detail, "Docker Hub") {
		t.Errorf("detail = %q, want it to say the sidecars pull from Docker Hub meanwhile, so nobody thinks jobs fail", got.Detail)
	}
}

func TestAMirrorWithNothingWrongIsNoProblem(t *testing.T) {
	h := newHarness(t)
	h.dindPool(h.installation(), "dind", nil)
	host := h.hostWith("builder")
	h.mirrorBeat(host.ID, &backend.RegistryMirrorReport{Stats: registrymirror.Stats{Requests: 5}})
	if hasCode(h.problemCodes(), "host.registry_mirror_degraded") {
		t.Fatal("raised for a mirror with nothing wrong")
	}
}

// A pool that runs an image of its own is not given the mirror, so a mirror that
// is broken costs it nothing, and so does a host whose own image is custom.
func TestABrokenMirrorCostsNothingToAPoolThatDoesNotUseIt(t *testing.T) {
	for name, tc := range map[string]struct {
		pool func(*store.Pool)
		host bool
	}{
		"the pool's own sidecar image": {func(p *store.Pool) { p.DinDImage = "registry.example.com/ci/dind:27" }, false},
		"the host's own sidecar image": {nil, true},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.dindPool(h.installation(), "dind", tc.pool)
			host := h.hostWith("builder")
			h.mirrorBeat(host.ID, &backend.RegistryMirrorReport{Problem: backend.MirrorProblemFolder, HostImageCustom: tc.host})
			if hasCode(h.problemCodes(), "host.registry_mirror_degraded") {
				t.Fatal("raised for a pool the mirror is not used for")
			}
		})
	}
}

func TestADisabledOrNonDockerPoolIsNotCountedAsHavingLostAMirror(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	h.dindPool(inst, "off", func(p *store.Pool) { p.Enabled = false })
	h.dindPool(inst, "podman", func(p *store.Pool) { p.Backend = store.BackendPodman })
	host := h.hostWith("builder")
	h.mirrorBeat(host.ID, &backend.RegistryMirrorReport{Problem: backend.MirrorProblemGateway})
	if hasCode(h.problemCodes(), "host.registry_mirror_degraded") {
		t.Fatal("raised for pools that are off or cannot run a docker-in-docker sidecar")
	}
}

// Every word an agent uses has its own sentence, and a word this controller has
// never heard is not shown as it arrived.
func TestEveryWordAnAgentUsesHasASentenceHereAndAStrangeOneIsNotEchoed(t *testing.T) {
	words := []string{
		backend.MirrorProblemContainer, backend.MirrorProblemSharedFolder, backend.MirrorProblemFolder,
		backend.MirrorProblemGateway, backend.MirrorProblemListener, backend.MirrorProblemUpstream,
	}
	seen := map[string]string{}
	for _, word := range words {
		h := newHarness(t)
		h.dindPool(h.installation(), "dind", nil)
		host := h.hostWith("builder")
		h.mirrorBeat(host.ID, &backend.RegistryMirrorReport{Problem: word})
		got := h.problem(t, "host.registry_mirror_degraded")
		if got.Detail == "" || got.Fix == "" {
			t.Errorf("%s: problem = %+v, want a detail and a fix", word, got)
		}
		if other, dup := seen[got.Detail]; dup {
			t.Errorf("%s has the same sentence as %s", word, other)
		}
		seen[got.Detail] = word
	}

	h := newHarness(t)
	h.dindPool(h.installation(), "dind", nil)
	host := h.hostWith("builder")
	const hostile = "<script>alert(1)</script> `rm -rf /`"
	h.mirrorBeat(host.ID, &backend.RegistryMirrorReport{Problem: hostile})
	got := h.problem(t, "host.registry_mirror_degraded")
	for _, text := range []string{got.Title, got.Detail, got.Fix} {
		if strings.Contains(text, "<script>") || strings.Contains(text, "alert(1)") || strings.Contains(text, "rm -rf") {
			t.Fatalf("an agent's own words were shown: %q", text)
		}
	}
	if got.Detail == "" || got.Fix == "" {
		t.Errorf("problem = %+v, want a general sentence for a problem this controller does not know", got)
	}
}

// What one beat says is true of that beat: a host that stops reporting a mirror,
// because the setting was turned off or the agent was rolled back, must stop
// being told off for one, as a host that stops reporting caches stops being
// shown them.
func TestAHostThatStopsReportingAMirrorStopsBeingToldOffForIt(t *testing.T) {
	h := newHarness(t)
	h.dindPool(h.installation(), "dind", nil)
	host := h.hostWith("builder")
	h.mirrorBeat(host.ID, &backend.RegistryMirrorReport{Problem: backend.MirrorProblemListener})
	if !hasCode(h.problemCodes(), "host.registry_mirror_degraded") {
		t.Fatal("not raised")
	}
	h.mirrorBeat(host.ID, nil)
	if hasCode(h.problemCodes(), "host.registry_mirror_degraded") {
		t.Fatal("still raised after the host stopped reporting a mirror")
	}
}

func TestAProblemThatEndsIsNoLongerRaised(t *testing.T) {
	h := newHarness(t)
	h.dindPool(h.installation(), "dind", nil)
	host := h.hostWith("builder")
	h.mirrorBeat(host.ID, &backend.RegistryMirrorReport{Problem: backend.MirrorProblemUpstream})
	if !hasCode(h.problemCodes(), "host.registry_mirror_degraded") {
		t.Fatal("not raised")
	}
	h.mirrorBeat(host.ID, &backend.RegistryMirrorReport{})
	if hasCode(h.problemCodes(), "host.registry_mirror_degraded") {
		t.Fatal("still raised after the agent said the problem had ended")
	}
}

func TestAReportForAHostThatNoLongerExistsIsForgotten(t *testing.T) {
	h := newHarness(t)
	host := h.hostWith("builder")
	gone := h.hostWith("gone")
	h.mirrorBeat(host.ID, &backend.RegistryMirrorReport{})
	h.mirrorBeat(gone.ID, &backend.RegistryMirrorReport{})
	got := h.c.registryMirrorReportsFor(map[string]bool{host.ID: true})
	if _, ok := got[host.ID]; !ok || len(got) != 1 {
		t.Fatalf("reports = %v, want only the host that is still there", got)
	}
	if again := h.c.registryMirrorReportsFor(map[string]bool{host.ID: true, gone.ID: true}); len(again) != 1 {
		t.Fatalf("a deleted host's report came back: %v", again)
	}
}

// pool.registry_mirror_unused is the one place an operator who turned the
// mirror on learns that one of their pools will not use it, and why.
func TestAPoolThatCannotUseTheMirrorIsToldSo(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	pool := h.dindPool(inst, "custom", func(p *store.Pool) { p.DinDImage = "registry.example.com/ci/dind:27" })
	h.dindPool(inst, "plain", nil)
	host := h.hostWith("builder")

	if hasCode(h.problemCodes(), "pool.registry_mirror_unused") {
		t.Fatal("raised with no mirror on anywhere")
	}
	h.mirrorBeat(host.ID, &backend.RegistryMirrorReport{})
	got := h.problem(t, "pool.registry_mirror_unused")
	if got.TargetKind != "pool" || got.TargetID != pool.ID || got.Severity != config.SeverityInfo {
		t.Fatalf("problem = %+v, want information on the pool with its own image", got)
	}
	if !strings.Contains(got.Detail, "dind_image") {
		t.Errorf("detail = %q, want it to name the setting that is the reason", got.Detail)
	}
	count := 0
	ps, err := h.c.Problems(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range ps {
		if p.Code == "pool.registry_mirror_unused" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("%d pools were told the mirror is unused, want only the one with an image of its own", count)
	}
}

// A pool that follows the host's own image is not given the mirror where the
// host's image is custom, but it is given it on any host whose is not, and a
// pool somebody can use the mirror for is not told otherwise.
func TestAPoolThatSomeHostWouldGiveTheMirrorIsNotToldItGoesWithout(t *testing.T) {
	h := newHarness(t)
	h.dindPool(h.installation(), "follows-host", nil)
	custom, plain := h.hostWith("custom"), h.hostWith("plain")
	h.mirrorBeat(custom.ID, &backend.RegistryMirrorReport{HostImageCustom: true})
	if !hasCode(h.problemCodes(), "pool.registry_mirror_unused") {
		t.Fatal("not raised for a pool whose only mirrored host has an image of its own")
	}
	h.mirrorBeat(plain.ID, &backend.RegistryMirrorReport{})
	if hasCode(h.problemCodes(), "pool.registry_mirror_unused") {
		t.Fatal("raised for a pool that a host with the default image would give the mirror")
	}
}

func TestAPoolThatRunsNoDockerSidecarIsNeverToldTheMirrorIsUnused(t *testing.T) {
	h := newHarness(t)
	h.pool(h.installation(), "no-docker") // DockerNone
	host := h.hostWith("builder")
	h.mirrorBeat(host.ID, &backend.RegistryMirrorReport{})
	if hasCode(h.problemCodes(), "pool.registry_mirror_unused") {
		t.Fatal("raised for a pool with no sidecar")
	}
}
