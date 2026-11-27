package controller

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/eyupio/zoomies/internal/backend"
	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/naming"
	"github.com/eyupio/zoomies/internal/scheduler"
	"github.com/eyupio/zoomies/internal/store"
)

// A host's agent says what is true of its registry mirror in every heartbeat,
// because the controller cannot read a standalone agent's settings: they are on
// that agent's own host. The controller keeps the latest report in memory, as it
// does the cache services' figures, and raises two problems from it.
//
// What an agent sends is not trusted to be shown as it arrives, so the agent says
// what is wrong in one of a fixed set of words and every sentence an operator
// reads is this file's.

// noteRegistryMirror records what a host's agent said about its mirror. A beat
// that says nothing clears the host: the setting was turned off, or the agent was
// rolled back to a release that does not know the mirror, and holding the old
// report would go on telling an operator about a mirror that is not there.
func (c *Controller) noteRegistryMirror(hostID string, rep *backend.RegistryMirrorReport) {
	c.mirrorMu.Lock()
	defer c.mirrorMu.Unlock()
	if rep == nil {
		delete(c.mirrorHosts, hostID)
		return
	}
	if c.mirrorHosts == nil {
		c.mirrorHosts = map[string]backend.RegistryMirrorReport{}
	}
	c.mirrorHosts[hostID] = *rep
}

// registryMirrorReportsFor is the latest report of each host in live, forgetting
// every other host: a deleted host would otherwise stand in the map for the life
// of the process.
func (c *Controller) registryMirrorReportsFor(live map[string]bool) map[string]backend.RegistryMirrorReport {
	c.mirrorMu.Lock()
	defer c.mirrorMu.Unlock()
	for id := range c.mirrorHosts {
		if !live[id] {
			delete(c.mirrorHosts, id)
		}
	}
	return maps.Clone(c.mirrorHosts)
}

// poolUsesDockerSidecar is whether a pool starts a docker-in-docker sidecar on a
// Docker daemon, which is the only thing a registry mirror serves.
func poolUsesDockerSidecar(p *store.Pool) bool {
	return p.Enabled && p.Backend == store.BackendDocker && p.DockerMode == store.DockerDinD
}

// poolGetsMirrorOn is whether a pool's sidecars on a host that reports rep are
// started with the mirror: only when they run the default image, because a mirror
// named beside a daemon.json of the operator's own stops the daemon starting. The
// pool's own image wins over the host's, and one that names the default itself is
// still the default; the backend decides it by the same rule, held to this one by
// a test.
func poolGetsMirrorOn(p *store.Pool, rep backend.RegistryMirrorReport) bool {
	return poolUsesDockerSidecar(p) && backend.SidecarImageIsDefault(p.DinDImage, rep.HostImageCustom)
}

// registryMirrorProblems gathers host.registry_mirror_degraded and
// pool.registry_mirror_unused.
func (c *Controller) registryMirrorProblems(ctx context.Context, out *[]Problem) error {
	hosts, err := c.fleetHosts(ctx)
	if err != nil {
		return fmt.Errorf("listing hosts: %w", err)
	}
	pools, err := c.fleetPools(ctx)
	if err != nil {
		return fmt.Errorf("listing pools: %w", err)
	}
	live := make(map[string]bool, len(hosts))
	for _, h := range hosts {
		live[h.ID] = true
	}
	reports := c.registryMirrorReportsFor(live)
	if len(reports) == 0 {
		return nil
	}
	for _, h := range hosts {
		rep, ok := reports[h.ID]
		if !ok || h.Retired {
			continue
		}
		if p, ok := registryMirrorDegradedProblem(h, rep, pools); ok {
			*out = append(*out, p)
		}
	}
	for _, p := range pools {
		if prob, ok := registryMirrorUnusedProblem(p, hosts, reports); ok {
			*out = append(*out, prob)
		}
	}
	return nil
}

// mirrorProblemSentences is what each word an agent uses means and what to do
// about it. Every one says that the sidecars pull from Docker Hub meanwhile, so
// that nobody reads "degraded" as "jobs are failing": a broken mirror slows a
// pull and never fails a job.
var mirrorProblemSentences = map[string]struct{ detail, fix string }{
	backend.MirrorProblemContainer: {
		"the agent runs in a container, and the registry mirror listens on the host's side of the network the sidecars are on, which a containerised agent is not.",
		"run the agent on the host itself, or turn agent.registry_mirror.enabled off on it to stop this.",
	},
	backend.MirrorProblemSharedFolder: {
		"the host has no shared folder to keep the registry mirror in.",
		"mount the shared folder into the agent's container from the host, as host.shared_folder_unmounted describes, or turn agent.registry_mirror.enabled off on it.",
	},
	backend.MirrorProblemFolder: {
		"the registry mirror could not make or write its folder, registry-mirror, in the shared folder.",
		"give the account the agent runs as write access to the shared folder and free disk space on it; the next runner that starts a sidecar tries again, and this clears.",
	},
	backend.MirrorProblemGateway: {
		"the agent could not find the host's address on the network the sidecars are on, so the registry mirror has nowhere to listen.",
		"check that the network the pool or agent.network names exists and has an IPv4 gateway; the next runner that starts a sidecar tries again.",
	},
	backend.MirrorProblemListener: {
		"the registry mirror could not listen on the host's address on the sidecars' network, or stopped listening.",
		"look in the agent's log for the reason, such as an address the host does not have; the next runner that starts a sidecar starts it again, and this clears.",
	},
	backend.MirrorProblemUpstream: {
		"Docker Hub has failed the registry mirror for at least five minutes without a break, so every pull waits for it to fail first.",
		"check that the host can reach registry-1.docker.io and auth.docker.io, through its proxy if it has one, and whether Docker Hub is limiting this host's address; the mirror recovers on its own once it is answered.",
	},
}

// mirrorProblemWords is what a host's view and its finding say for a word an agent
// used: the word itself when this controller knows it, and "other" with a general
// sentence when it does not, because the agent's text is not trusted to be shown
// as it arrived. The fix for a word this controller has never heard is the one
// thing it can say for certain.
func mirrorProblemWords(word string) (shown string, detail, fix string) {
	if sentence, known := mirrorProblemSentences[word]; known {
		return word, sentence.detail, sentence.fix
	}
	return "other",
		"the agent reports a problem with its registry mirror that this controller has no description for.",
		"upgrade the controller to the same release as the agent, or look in the agent's log."
}

// RegistryMirrorView is what a host's registry mirror is doing, for the host card.
// Problem is empty when nothing is wrong. Detail and Fix are the controller's
// sentences, so the page does not keep a copy that could drift from the finding's,
// and HostImageCustom says that pools which follow the host's own sidecar image
// are not given the mirror.
type RegistryMirrorView struct {
	Problem         string `json:"problem,omitempty"`
	Detail          string `json:"detail,omitempty"`
	Fix             string `json:"fix,omitempty"`
	HostImageCustom bool   `json:"host_image_custom,omitempty"`
}

// registryMirrorView is the mirror's part of a host's view, nil while the host
// has not said it has one on.
func (c *Controller) registryMirrorView(hostID string) *RegistryMirrorView {
	c.mirrorMu.Lock()
	rep, ok := c.mirrorHosts[hostID]
	c.mirrorMu.Unlock()
	if !ok {
		return nil
	}
	out := &RegistryMirrorView{HostImageCustom: rep.HostImageCustom}
	if rep.Problem != "" {
		out.Problem, out.Detail, out.Fix = mirrorProblemWords(rep.Problem)
	}
	return out
}

// registryMirrorDegradedProblem is host.registry_mirror_degraded: the agent says
// something is wrong with its mirror, raised only where it costs something, which
// is a pool that would have been given the mirror and could run on this host.
func registryMirrorDegradedProblem(h *store.Host, rep backend.RegistryMirrorReport, pools []*store.Pool) (Problem, bool) {
	if rep.Problem == "" {
		return Problem{}, false
	}
	var affected []string
	for _, p := range pools {
		if poolGetsMirrorOn(p, rep) && scheduler.HostCouldRun(h, p) {
			affected = append(affected, p.Name)
		}
	}
	if len(affected) == 0 {
		return Problem{}, false
	}
	_, detail, fix := mirrorProblemWords(rep.Problem)
	return Problem{
		Code:     "host.registry_mirror_degraded",
		Severity: config.SeverityWarning,
		Title:    naming.ForSentence(h.Name) + " is not serving its registry mirror",
		Detail: fmt.Sprintf("%s %s start their docker-in-docker sidecars there without it and pull from Docker Hub as they did before, so nothing fails; the pulls are slower and count against Docker Hub's limit.",
			capitalise(detail), strings.Join(affected, ", ")),
		Fix:        fix,
		TargetKind: "host",
		TargetID:   h.ID,
	}, true
}

// registryMirrorUnusedProblem is pool.registry_mirror_unused: a mirror is on
// somewhere that could run this pool and none of those hosts would give it the
// mirror, because of an image the operator chose. It is information, since the
// operator chose that image on purpose and nothing is wrong, but it is the only
// place they learn that turning the mirror on did nothing for this pool.
func registryMirrorUnusedProblem(p *store.Pool, hosts []*store.Host, reports map[string]backend.RegistryMirrorReport) (Problem, bool) {
	if !poolUsesDockerSidecar(p) {
		return Problem{}, false
	}
	var onHosts []string
	for _, h := range hosts {
		rep, ok := reports[h.ID]
		if !ok || h.Retired || !scheduler.HostCouldRun(h, p) {
			continue
		}
		if poolGetsMirrorOn(p, rep) {
			return Problem{}, false
		}
		onHosts = append(onHosts, naming.ForSentence(h.Name))
	}
	if len(onHosts) == 0 {
		return Problem{}, false
	}
	slices.Sort(onHosts)
	var why string
	if strings.TrimSpace(p.DinDImage) != "" {
		why = "the pool's own dind_image"
	} else {
		why = "agent.dind_image on the host"
	}
	return Problem{
		Code:     "pool.registry_mirror_unused",
		Severity: config.SeverityInfo,
		Title:    "the registry mirror is not used for " + naming.ForSentence(p.Name),
		Detail: fmt.Sprintf("the registry mirror is on at %s, but this pool's docker-in-docker sidecars run an image chosen by %s, which is where an operator keeps a daemon.json of their own, and a mirror named in two places stops the daemon starting. They pull from Docker Hub as before.",
			strings.Join(onHosts, ", "), why),
		Fix:        "nothing, if the image is meant: bake a registry mirror into it as docs/persistent-caches.md describes. Otherwise clear the image so the pool uses the default sidecar, which the mirror serves.",
		TargetKind: "pool",
		TargetID:   p.ID,
	}, true
}
