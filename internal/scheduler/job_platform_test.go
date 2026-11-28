package scheduler

import (
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/store"
)

func TestQueuedJobsArePlacedOnHostsThatCanAdvertiseTheirPlatformLabels(t *testing.T) {
	pool := testPool("ci", "ci")
	amd := hostOn("host_a", "ubuntu", "24.04", "amd64", 4)
	arm := hostOn("host_b", "ubuntu", "24.04", "arm64", 1)
	// The backend's platform can differ from the host running the agent.
	arm.OS, arm.Distro, arm.Arch = "darwin", "", "amd64"
	arm.BackendInfo = store.HostBackends{{Kind: store.BackendDocker, Available: true, OS: "linux", Arch: "arm64"}}
	armJob := queued("arm", 2*time.Minute, "self-hosted", "Linux", "ARM64", "ci")
	x64Job := queued("x64", time.Minute, "self-hosted", "linux", "x64", "ci")

	for _, tc := range []struct {
		name        string
		hosts       []*store.Host
		jobs        []*store.Job
		wantHosts   []string
		wantBlocked bool
	}{
		{"mixed architecture", []*store.Host{amd, arm}, []*store.Job{armJob, x64Job}, []string{arm.ID, amd.ID}, false},
		{"no compatible host", []*store.Host{amd}, []*store.Job{armJob}, nil, true},
		{"blocked job does not hold up a compatible job", []*store.Host{amd}, []*store.Job{armJob, x64Job}, []string{amd.ID}, true},
		{"job with only a pool label", []*store.Host{amd}, []*store.Job{queued("any", time.Minute, "ci")}, []string{amd.ID}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan := Decide(snap([]*store.Pool{pool}, nil, tc.jobs, tc.hosts))
			got := actionsOf(plan.Actions, ActionCreate)
			if len(got) != len(tc.wantHosts) {
				t.Fatalf("creates = %+v, want hosts %v", got, tc.wantHosts)
			}
			for i, host := range tc.wantHosts {
				if got[i].HostID != host {
					t.Errorf("create %d went to %q, want %q", i, got[i].HostID, host)
				}
			}
			if tc.wantBlocked && (!strings.Contains(plan.Pools[0].Blocked, "arm64") || plan.Pools[0].Unserved != 1) {
				t.Errorf("want one blocked ARM64 job, got %+v", plan.Pools[0])
			}
		})
	}
}

func TestAnIdleRunnerForTheWrongArchitectureDoesNotCountAsSupply(t *testing.T) {
	pool := testPool("ci", "ci")
	amd := hostOn("host_a", "ubuntu", "24.04", "amd64", 4)
	arm := hostOn("host_b", "ubuntu", "24.04", "arm64", 1)
	runner := idleRunner("amd-warm", pool, time.Second)
	job := queued("arm", time.Minute, "arm64", "ci")
	for _, maxRunners := range []int{1, 2} {
		pool.MaxRunners = maxRunners
		plan := Decide(snap([]*store.Pool{pool}, []*store.Runner{runner}, []*store.Job{job}, []*store.Host{amd, arm}))
		if maxRunners == 1 {
			drains := actionsOf(plan.Actions, ActionDrain)
			if len(drains) != 1 || drains[0].RunnerID != runner.ID || len(actionsOf(plan.Actions, ActionCreate)) != 0 {
				t.Fatalf("a full pool should release the incompatible runner first, got %+v", plan.Actions)
			}
			continue
		}
		creates := actionsOf(plan.Actions, ActionCreate)
		if len(creates) != 1 || creates[0].HostID != arm.ID {
			t.Fatalf("want one ARM64 runner alongside the idle x64 runner, got %+v", plan.Actions)
		}
	}
}

func TestCoveredPlatformDemandStillLetsSurplusRunnersScaleDown(t *testing.T) {
	pool := testPool("ci", "ci")
	host := hostOn("host_a", "ubuntu", "24.04", "arm64", 4)
	runners := []*store.Runner{
		idleRunner("first", pool, 6*time.Minute),
		idleRunner("second", pool, 7*time.Minute),
	}
	job := queued("arm", time.Minute, "arm64", "ci")
	plan := Decide(snap([]*store.Pool{pool}, runners, []*store.Job{job}, []*store.Host{host}))
	if got := len(actionsOf(plan.Actions, ActionDrain)); got != 1 || plan.Pools[0].Desired != 1 {
		t.Fatalf("one runner should stay for the job and one should drain, got %+v", plan.Pools[0])
	}
}

func TestScaleDownKeepsAStartingRunnerThatCanTakeTheQueuedJob(t *testing.T) {
	pool := testPool("ci", "ci")
	amd := hostOn("host_a", "ubuntu", "24.04", "amd64", 4)
	arm := hostOn("host_b", "ubuntu", "24.04", "arm64", 4)
	idle := idleRunner("idle-x64", pool, 6*time.Minute)
	for _, state := range []store.RunnerState{store.RunnerProvisioning, store.RunnerRegistering, store.RunnerIdle} {
		t.Run(string(state), func(t *testing.T) {
			matching := testRunner("arm64", pool, state, time.Minute)
			matching.HostID = arm.ID
			job := queued("arm", time.Minute, "arm64", "ci")
			plan := Decide(snap([]*store.Pool{pool}, []*store.Runner{idle, matching}, []*store.Job{job}, []*store.Host{amd, arm}))
			drains := actionsOf(plan.Actions, ActionDrain)
			if len(drains) != 1 || drains[0].RunnerID != idle.ID {
				t.Fatalf("want to drain only idle x64 runner %q, got %+v", idle.ID, drains)
			}
		})
	}
}

func TestPersonalAccountPlacementKeepsTheRepositoryOfTheCompatibleJob(t *testing.T) {
	pool := testPool("ci", "ci")
	host := hostOn("host_a", "ubuntu", "24.04", "amd64", 4)
	arm := queued("arm", 2*time.Minute, "arm64", "ci")
	arm.Repo = "acme/arm"
	x64 := queued("x64", time.Minute, "x64", "ci")
	x64.Repo = "acme/x64"
	s := snap([]*store.Pool{pool}, nil, []*store.Job{arm, x64}, []*store.Host{host})
	s.Installations[0].AccountType = store.AccountUser
	plan := Decide(s)
	creates := actionsOf(plan.Actions, ActionCreate)
	if len(creates) != 1 || creates[0].Repo != x64.Repo {
		t.Fatalf("want a runner for %s, got %+v", x64.Repo, creates)
	}
}

// A job that asks for a platform no host has is not a pool that any new machine
// would unblock: the machine loop rents the provider's own shape for a pool
// reported as having nowhere to go, so ten arm64 jobs on an amd64 fleet rented
// amd64 machines that could never run them, held until the jobs went stale.
func TestAJobAskingForAPlatformNoHostHasDoesNotSayAnyMachineWouldDo(t *testing.T) {
	pool := testPool("ci", "ci")
	amd := hostOn("host_a", "ubuntu", "24.04", "amd64", 4)
	pp := only(t, Decide(snap([]*store.Pool{pool}, nil, []*store.Job{queued("arm", time.Minute, "arm64", "ci")}, []*store.Host{amd})))
	if !strings.Contains(pp.Blocked, "arm64") {
		t.Fatalf("blocked = %q, want it to name the labels no host supplies", pp.Blocked)
	}
	if pp.BlockedNoEligibleHost || pp.BlockedAtCapacity {
		t.Fatalf("no eligible host = %v, at capacity = %v; want neither, so nothing is rented for it",
			pp.BlockedNoEligibleHost, pp.BlockedAtCapacity)
	}
}

// The surplus is counted against what the repository-aware decision settled
// on, not the pool's total: a personal account's runners are not
// interchangeable, so an idle runner for one repository is no reason to stop
// the runner starting for another repository's queued job.
func TestARunnerStartingForARepositorysJobIsNotSurplusBesideAnotherRepositorysIdleRunner(t *testing.T) {
	pool := testPool("ci", "ci")
	host := hostOn("host_a", "ubuntu", "24.04", "amd64", 4)
	idle := idleRunner("idle-a", pool, time.Second)
	idle.Repo = "acme/a"
	starting := testRunner("starting-b", pool, store.RunnerProvisioning, 3*time.Second)
	starting.Repo = "acme/b"
	job := queued("b", time.Minute, "ci")
	job.Repo = "acme/b"
	s := snap([]*store.Pool{pool}, []*store.Runner{idle, starting}, []*store.Job{job}, []*store.Host{host})
	s.Installations[0].AccountType = store.AccountUser
	if drains := actionsOf(Decide(s).Actions, ActionDrain); len(drains) != 0 {
		t.Fatalf("drained %+v, want nothing: the job on acme/b is still waiting for its runner", drains)
	}
}
