package controller

import (
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
