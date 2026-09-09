//go:build drill

package drill

import (
	"strings"
	"testing"
)

// The fifth fault drill: a host whose Docker socket is not there.
//
// This is the fault an operator is most likely to cause by accident -- a
// daemon that did not come back after a reboot, a socket a hardening pass
// moved, an agent running as a user that is not in the `docker` group -- and
// the one where saying nothing is worst. The jobs still queue, the pool still
// looks configured, and a fleet that quietly places nothing is
// indistinguishable from a fleet with nothing to do.
//
// So the drill asks for the two things a person needs and nothing more: the
// host says which backend is unusable and why, and the pool that cannot place
// anything says so as a problem an operator would see, with a fix.
//
// What it deliberately does not cover is the recovery half. Starting a daemon
// needs a daemon, and this tier has none -- that is what makes it something
// every pull request can afford. A socket that is not there is exactly what a
// stopped daemon looks like from the outside, and that half is honest here;
// the rest belongs on the reference host the roadmap asks the owner for.
func TestAHostWithNoDockerSocketSaysWhichBackendIsUnusable(t *testing.T) {
	f := newFleet(t)
	rec := newRecord(t, "docker-socket")
	defer rec.write()

	const name = "drill-docker-host"
	rec.faultInjected("joined a host whose Docker socket is not there")
	f.startBrokenDockerAgent(name)

	// It joins. An agent that refused to start on a host with no daemon would
	// take the fleet's only way of telling anybody about it with it.
	var broken hostView
	waitFor(t, waitProcessUp, "the host with no Docker to appear", func() bool {
		for _, h := range f.hostViews() {
			if h.Name == name {
				broken = h
				return true
			}
		}
		return false
	})
	rec.note("host joined", broken.ID)

	// And it says which backend is unusable, where it looked, and why -- the
	// three things that turn "it is not working" into something to go and fix.
	var docker struct {
		found     bool
		available bool
		endpoint  string
		detail    string
	}
	for _, b := range broken.BackendInfo {
		if b.Kind == "docker" {
			docker.found, docker.available = true, b.Available
			docker.endpoint, docker.detail = b.Endpoint, b.Detail
		}
	}
	if !docker.found {
		t.Fatalf("the host reports no docker backend at all: %+v", broken.BackendInfo)
	}
	if docker.available {
		t.Fatalf("the host says its docker backend is available, with no socket at %s", docker.endpoint)
	}
	if !strings.Contains(docker.endpoint, "zoomies-drill-docker.sock") {
		t.Errorf("the host does not say where it looked: endpoint %q", docker.endpoint)
	}
	if docker.detail == "" {
		t.Errorf("the host says the backend is unavailable and not why; an operator has nothing to act on")
	}
	rec.note("host says the backend is unusable", docker.detail)

	// A pool on that backend with work waiting is the case that must not be
	// silent. It is an error rather than a warning because there is nothing
	// here that time will fix.
	poolID := f.createPoolOn("drilldocker", "docker", "drill-docker")
	f.gh.AddQueuedJob("acme/api", "CI", "build", []string{"self-hosted", "drill-docker"})

	var problem problemView
	waitFor(t, waitRunnerCreated, "the fleet to raise a problem for the pool that cannot place", func() bool {
		for _, p := range f.problems() {
			if p.Code == "pool.no_capacity" && strings.Contains(p.Title, "drilldocker") {
				problem = p
				return true
			}
		}
		return false
	})
	if problem.Severity != "error" {
		t.Errorf("a pool with work waiting and nowhere to run it is %q; want error, because nothing here clears on its own", problem.Severity)
	}
	if problem.Detail == "" || problem.Fix == "" {
		t.Errorf("the problem says what is wrong without saying what to do: %+v", problem)
	}
	rec.note("problem raised", problem.Title+" -- "+problem.Detail)

	// And nothing was started anyway. A fleet that answered an unusable
	// backend by trying would leave half-made runners behind on every pass.
	if runners := f.runners(poolID); len(runners) != 0 {
		t.Errorf("the fleet created %d runner(s) for a pool whose backend is unusable", len(runners))
	}
	if live := f.liveWorkloads(); len(live) != 0 {
		t.Errorf("the host holds %v for a backend that is not there", live)
	}
	rec.note("started anyway", "nothing")
	rec.pass("a host whose Docker socket is missing joined, named the backend it could not use and where it looked, and the pool that could not place raised an error saying what to do")
}
