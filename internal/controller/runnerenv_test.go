package controller

import (
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/agent"
	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/store"
)

// The fleet's runner settings reach a runner through its create task, with
// the pool's own env over the top: one setting on the Settings page is what
// every pool gets, and a pool that says otherwise is believed.
func TestRunnerEnvLayersTheFleetUnderThePool(t *testing.T) {
	fleet := config.Runners{
		DockerWait: 90 * time.Second,
		Env:        map[string]string{"HTTPS_PROXY": "http://fleet-proxy:3128", "GOFLAGS": "-mod=mod"},
	}
	pool := &store.Pool{DockerMode: store.DockerDinD, Env: map[string]string{"HTTPS_PROXY": "http://pool-proxy:3128"}}

	got := runnerEnv(fleet, pool)
	if got["HTTPS_PROXY"] != "http://pool-proxy:3128" {
		t.Fatalf("the pool's proxy lost to the fleet's: %v", got)
	}
	if got["GOFLAGS"] != "-mod=mod" {
		t.Fatalf("the fleet's variable did not reach the runner: %v", got)
	}
	if got[EnvDockerWait] != "90" {
		t.Fatalf("docker wait = %q, want whole seconds", got[EnvDockerWait])
	}

	// A pool with no daemon has nothing to wait for, and a zero wait leaves
	// the image's own default rather than handing it a value it refuses.
	pool.DockerMode = store.DockerNone
	if _, set := runnerEnv(fleet, pool)[EnvDockerWait]; set {
		t.Fatal("a pool with no daemon was told how long to wait for one")
	}
	pool.DockerMode = store.DockerHostSocket
	fleet.DockerWait = 0
	if _, set := runnerEnv(fleet, pool)[EnvDockerWait]; set {
		t.Fatal("a zero wait was written, which the image refuses")
	}
	fleet.DockerWait = 300 * time.Millisecond
	if got := runnerEnv(fleet, pool)[EnvDockerWait]; got != "1" {
		t.Fatalf("a wait under a second became %q, want 1", got)
	}
	// The pool's own ZOOMIES_DOCKER_WAIT is still the last word.
	fleet.DockerWait = 2 * time.Minute
	pool.Env[EnvDockerWait] = "600"
	if got := runnerEnv(fleet, pool)[EnvDockerWait]; got != "600" {
		t.Fatalf("the pool's own wait lost to the fleet's: %q", got)
	}
	if runnerEnv(config.Runners{}, &store.Pool{}) != nil {
		t.Fatal("nothing to say became an empty map rather than none")
	}
}

// End to end: the setting changed on the Settings page is in the next create
// task's spec, so it is live in the sense the registry promises.
func TestACreateTaskCarriesTheFleetsRunnerEnvironment(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	pool := h.pool(inst, "builders")
	pool.DockerMode = store.DockerDinD
	if err := h.st.UpdatePool(h.ctx, pool); err != nil {
		t.Fatalf("UpdatePool: %v", err)
	}
	host := h.host("vm-1")
	h.c.UpdateConfig(func(c *config.Config) {
		c.Runners.DockerWait = 3 * time.Minute
		c.Runners.Env = map[string]string{"NO_PROXY": "localhost"}
	})

	h.deliverJob(jobEvent{Action: "queued", JobID: 11, Labels: []string{"self-hosted", "linux", "x64", "demo"}})
	if err := h.c.Reconcile(h.ctx); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	h.c.lifecycleCalls.Wait()
	task := h.taskOfKind(host.ID, agent.TaskCreateRunner)
	if task.Spec == nil {
		t.Fatal("no create task")
	}
	if task.Spec.Env[EnvDockerWait] != "180" || task.Spec.Env["NO_PROXY"] != "localhost" {
		t.Fatalf("create task env = %v", task.Spec.Env)
	}
}
