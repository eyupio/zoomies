package controller

import (
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/store"
)

func poolDur(d time.Duration) *store.Duration {
	v := store.Duration(d)
	return &v
}

func warned(problems []Problem, code string) *Problem {
	for i := range problems {
		if problems[i].Code == code {
			return &problems[i]
		}
	}
	return nil
}

// A pool that overrides its provision timeout can re-create, for itself, the
// ladder inversion the fleet's own defaults are held out of.
//
// The fleet's three budgets are kept in order by an invariant test and by the
// validator, and neither of those reaches a pool: a pool may now set the
// provision timeout, the Docker wait, or one without the other. A pool whose
// timeout lands inside the start its own settings ask for condemns runners
// that are still coming up, and the replacement pulls the same image over the
// link that was slow to begin with -- the same outage as the fleet-wide one,
// on the pool that overrode it.
func TestAPoolCannotQuietlyCondemnItsOwnStartingRunners(t *testing.T) {
	cfg := config.Default()

	base := func() *store.Pool {
		return &store.Pool{
			ID: "pool_x", Name: "zoomies-slow", Enabled: true,
			Backend: store.BackendDocker, DockerMode: store.DockerDinD,
		}
	}

	// Following the fleet says nothing: the fleet's own figures are in order,
	// and a warning on every correct pool is one an operator stops reading.
	if w := warned(PoolWarnings(base(), nil, cfg), "pool.provision_timeout_short"); w != nil {
		t.Fatalf("a pool that overrides nothing warned about the fleet's own order: %s", w.Title)
	}

	// A timeout inside the create budget plus this pool's Docker wait does.
	p := base()
	p.RunnerSettings.ProvisionTimeout = poolDur(5 * time.Minute)
	w := warned(PoolWarnings(p, nil, cfg), "pool.provision_timeout_short")
	if w == nil {
		t.Fatal("a 5m provision timeout on a pool whose runners wait 2m for Docker after a 15m create budget raised nothing")
	}
	if w.Fix == "" || w.TargetID != p.ID {
		t.Errorf("the warning does not say what to do or which pool it is about: %+v", w)
	}

	// Raised above what a start actually costs, it goes quiet again.
	p.RunnerSettings.ProvisionTimeout = poolDur(30 * time.Minute)
	if w := warned(PoolWarnings(p, nil, cfg), "pool.provision_timeout_short"); w != nil {
		t.Errorf("a 30m timeout still warned: %s", w.Title)
	}

	// Zero is "never give up", which cannot condemn anything.
	p.RunnerSettings.ProvisionTimeout = poolDur(0)
	if w := warned(PoolWarnings(p, nil, cfg), "pool.provision_timeout_short"); w != nil {
		t.Errorf("a pool that asked for no timeout at all warned: %s", w.Title)
	}
}

// The wait counts only where the pool actually does it. A pool with no daemon
// waits for none, so its start costs the create budget alone -- and a timeout
// that clears that is right, whatever a Docker pool would have needed.
func TestOnlyAPoolWithADaemonIsChargedTheDockerWait(t *testing.T) {
	cfg := config.Default()
	p := &store.Pool{
		ID: "pool_y", Name: "zoomies-plain", Enabled: true,
		Backend: store.BackendDocker, DockerMode: store.DockerNone,
		RunnerSettings: store.RunnerSettings{ProvisionTimeout: poolDur(16 * time.Minute)},
	}
	if w := warned(PoolWarnings(p, nil, cfg), "pool.provision_timeout_short"); w != nil {
		t.Errorf("a pool with no daemon was charged a wait it never does: %s", w.Title)
	}

	// The same 16 minutes on a pool that does wait is inside its own start.
	p.DockerMode = store.DockerDinD
	if w := warned(PoolWarnings(p, nil, cfg), "pool.provision_timeout_short"); w == nil {
		t.Error("a pool that waits 2m for Docker after a 15m create budget was not warned about a 16m timeout")
	}
}

// A pool's own Docker wait is what its start is measured against, because it
// is what its runners actually do.
func TestAPoolsOwnDockerWaitIsWhatItsStartIsMeasuredAgainst(t *testing.T) {
	cfg := config.Default()
	p := &store.Pool{
		ID: "pool_z", Name: "zoomies-patient", Enabled: true,
		Backend: store.BackendDocker, DockerMode: store.DockerDinD,
		RunnerSettings: store.RunnerSettings{
			ProvisionTimeout: poolDur(20 * time.Minute),
			DockerWait:       poolDur(10 * time.Minute),
		},
	}
	// 15m of create budget plus this pool's own 10m wait is 25m, so 20m is inside it.
	w := warned(PoolWarnings(p, nil, cfg), "pool.provision_timeout_short")
	if w == nil {
		t.Fatal("a pool that waits 10m for Docker was measured against the fleet's 2m")
	}
	if got := PoolEffectiveDockerWait(p, cfg); got != 10*time.Minute {
		t.Errorf("effective docker wait = %s, want the pool's 10m", got)
	}

	// An override of zero hands the wait to the image, which waits two minutes
	// -- not to no wait at all, which is what a bare zero would read as.
	p.RunnerSettings.DockerWait = poolDur(0)
	if got := PoolEffectiveDockerWait(p, cfg); got != config.ImageDockerWait {
		t.Errorf("effective docker wait = %s, want the image's own %s", got, config.ImageDockerWait)
	}
}

// A pool's fixed size can strand the machine its hosts have above their slot
// count, and the info says so with the edit that would take it back.
//
// It is the state a fixed size drifts into as a fleet acquires unequal
// machines: the figure chosen for the first host fits four runners on the
// 64-core one that joined later, and nothing on any page says the pool is why.
// An automatic pool cannot be in that state at all -- dividing a machine by
// its slot count is what fills every slot -- so the info is only ever about a
// size somebody typed.
func TestAFixedSizeSaysWhatItLeavesUnused(t *testing.T) {
	fixed := &store.Pool{
		ID: "pool_fixed", Name: "zoomies-small", Enabled: true,
		Backend:   store.BackendDocker,
		Resources: store.Resources{CPUs: 2, MemoryMB: 4096},
	}
	// Four slots on a machine with room for sixteen runners of this size.
	room := PoolRoom{Hosts: []PoolHostRoom{{
		HostID: "host_1", Host: "big-1",
		Slots: 4, Fits: 16, Room: 4, LimitedBy: "slots",
		CPUs: 32, MemoryMB: 65536, CPUsKnown: true, MemoryKnown: true,
	}}}

	w := warned(PoolRoomWarnings(fixed, room), "pool.size_strands_hosts")
	if w == nil {
		t.Fatal("a 2 CPU pool on a host with room for sixteen of them said nothing")
	}
	if !strings.Contains(w.Detail, "big-1") {
		t.Errorf("the info does not name the host: %s", w.Detail)
	}
	if !strings.Contains(w.Fix, "one slot's share") {
		t.Errorf("the info does not offer the automatic size as the fix: %s", w.Fix)
	}

	// The same fleet, sized by its host, is not stranding anything: the share
	// is the machine divided by the slots, so every slot is usable.
	automatic := &store.Pool{
		ID: "pool_auto", Name: "zoomies-auto", Enabled: true, Backend: store.BackendDocker,
	}
	autoRoom := PoolRoom{Hosts: []PoolHostRoom{{
		HostID: "host_1", Host: "big-1",
		Slots: 4, Fits: 4, Room: 4,
		CPUs: 32, MemoryMB: 65536, CPUsKnown: true, MemoryKnown: true,
	}}}
	if w := warned(PoolRoomWarnings(automatic, autoRoom), "pool.size_strands_hosts"); w != nil {
		t.Errorf("an automatic pool was told its size strands capacity: %s", w.Title)
	}

	// Nor is a fixed pool whose hosts it fills. A note an operator sees on
	// every correct pool is one they stop reading.
	tight := PoolRoom{Hosts: []PoolHostRoom{{
		HostID: "host_2", Host: "right-1",
		Slots: 4, Fits: 4, Room: 4,
		CPUs: 8, MemoryMB: 16384, CPUsKnown: true, MemoryKnown: true,
	}}}
	if w := warned(PoolRoomWarnings(fixed, tight), "pool.size_strands_hosts"); w != nil {
		t.Errorf("a fixed pool that fills its hosts was told otherwise: %s", w.Title)
	}

	// A host that has measured nothing is placed by slots alone and has no
	// share to compare against, so it is never counted as stranded.
	unmeasured := PoolRoom{Hosts: []PoolHostRoom{{
		HostID: "host_3", Host: "old-1", Slots: 4, Fits: 99, Room: 4,
	}}}
	if w := warned(PoolRoomWarnings(fixed, unmeasured), "pool.size_strands_hosts"); w != nil {
		t.Errorf("a host that has measured nothing was counted as stranded: %s", w.Detail)
	}
}

// A pool that overrides neither half says nothing, even when the fleet's own
// figures are in a bad order.
//
// That case is the fleet's finding -- scheduler.provision_timeout_short, which
// names the setting to change -- and repeating it once per pool would bury the
// one sentence that matters under a row for every pool in the fleet, all of
// them pointing at the same fix.
func TestAPoolFollowingTheFleetLeavesTheLadderToTheFleet(t *testing.T) {
	cfg := config.Default()
	// A fleet whose own timeout is inside what a start costs. The validator
	// already says so about the setting.
	cfg.Scheduler.ProvisionTimeout = 5 * time.Minute

	following := &store.Pool{
		ID: "pool_follow", Name: "zoomies-follows", Enabled: true,
		Backend: store.BackendDocker, DockerMode: store.DockerDinD,
	}
	if w := warned(PoolWarnings(following, nil, cfg), "pool.provision_timeout_short"); w != nil {
		t.Errorf("a pool that overrides nothing repeated the fleet's own finding: %s", w.Title)
	}

	// A pool that overrode the Docker wait alone is its own case: the fleet's
	// timeout may have been fine against the fleet's wait and is not against
	// this pool's.
	longWait := &store.Pool{
		ID: "pool_wait", Name: "zoomies-patient", Enabled: true,
		Backend: store.BackendDocker, DockerMode: store.DockerDinD,
		RunnerSettings: store.RunnerSettings{DockerWait: poolDur(30 * time.Minute)},
	}
	if w := warned(PoolWarnings(longWait, nil, cfg), "pool.provision_timeout_short"); w == nil {
		t.Error("a pool that waits 30m for Docker under a 5m timeout said nothing")
	}
}
