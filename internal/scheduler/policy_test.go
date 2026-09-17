package scheduler

import (
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/store"
)

func dur(d time.Duration) *store.Duration {
	v := store.Duration(d)
	return &v
}

// A pool that overrides nothing follows the fleet, and keeps following it.
//
// This is what makes the fleet's figures worth changing: a pool that had them
// copied onto it at creation would be frozen on whatever they were that day,
// and an operator raising scheduler.provision_timeout for a fleet on a slow
// registry would raise it for no pool at all.
func TestAPoolThatOverridesNothingFollowsTheFleet(t *testing.T) {
	fleet := Policy{
		ScaleUpDelay:      10 * time.Second,
		MaxRunnerLifetime: 24 * time.Hour,
		ProvisionTimeout:  10 * time.Minute,
		DrainTimeout:      5 * time.Minute,
		MaxCreatesPerTick: 4,
	}
	got := fleet.For(&store.Pool{Name: "zoomies-plain"})
	if got != fleet {
		t.Errorf("For() = %+v, want the fleet's own %+v", got, fleet)
	}
	if got := fleet.For(nil); got != fleet {
		t.Errorf("For(nil) = %+v, want the fleet's own %+v", got, fleet)
	}
}

// Each override replaces exactly one figure and leaves the rest alone, so a
// pool that has something to say about registering does not thereby have
// something to say about draining.
func TestAnOverrideReplacesOneFigure(t *testing.T) {
	fleet := Policy{
		ScaleUpDelay:      10 * time.Second,
		MaxRunnerLifetime: 24 * time.Hour,
		ProvisionTimeout:  10 * time.Minute,
		DrainTimeout:      5 * time.Minute,
		MaxCreatesPerTick: 4,
	}
	pool := &store.Pool{Name: "zoomies-windows", RunnerSettings: store.RunnerSettings{
		ProvisionTimeout: dur(45 * time.Minute),
	}}
	got := fleet.For(pool)
	if got.ProvisionTimeout != 45*time.Minute {
		t.Errorf("provision timeout = %s, want the pool's 45m", got.ProvisionTimeout)
	}
	if got.DrainTimeout != fleet.DrainTimeout || got.ScaleUpDelay != fleet.ScaleUpDelay ||
		got.MaxRunnerLifetime != fleet.MaxRunnerLifetime {
		t.Errorf("one override changed the others: %+v", got)
	}
}

// Zero is a real answer rather than "unset", which is the whole reason these
// are pointers. A pool on an air-gapped registry that pulls for half an hour
// means "never fail a runner for taking too long to register", and a type that
// read its zero as "nothing said" would give it the fleet's ten minutes back.
func TestZeroIsAnAnswerAndNotAnAbsence(t *testing.T) {
	fleet := Policy{ProvisionTimeout: 10 * time.Minute, DrainTimeout: 5 * time.Minute}
	pool := &store.Pool{Name: "zoomies-airgapped", RunnerSettings: store.RunnerSettings{
		ProvisionTimeout: dur(0),
	}}
	if got := fleet.For(pool); got.ProvisionTimeout != 0 {
		t.Errorf("provision timeout = %s, want 0: the pool asked for no timeout at all", got.ProvisionTimeout)
	}
}

// The fleet-wide create budget is not a pool's to raise. It is shared between
// pools, so a pool that could lift its own share would be taking the
// protection from the others.
func TestTheCreateBudgetIsNotOverridable(t *testing.T) {
	fleet := Policy{MaxCreatesPerTick: 4}
	pool := &store.Pool{Name: "zoomies-greedy", RunnerSettings: store.RunnerSettings{
		ScaleUpDelay: dur(0),
	}}
	if got := fleet.For(pool); got.MaxCreatesPerTick != 4 {
		t.Errorf("max creates per tick = %d, want the fleet's 4", got.MaxCreatesPerTick)
	}
}

// Set is what every surface asks before it says a pool has overrides at all.
func TestSetSaysWhetherAPoolOverridesAnything(t *testing.T) {
	if (store.RunnerSettings{}).Set() {
		t.Error("an empty RunnerSettings reports itself as set")
	}
	if !(store.RunnerSettings{DockerWait: dur(2 * time.Minute)}).Set() {
		t.Error("a pool with a docker wait reports itself as unset")
	}
	if !(store.RunnerSettings{ProvisionTimeout: dur(0)}).Set() {
		t.Error("an override of zero reports itself as unset; zero is an answer")
	}
}
