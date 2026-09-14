package agent

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/eyupio/zoomies/internal/backend"
	"github.com/eyupio/zoomies/internal/store"
)

func TestStartupQueuePreservesForegroundOrderAheadOfRefreshes(t *testing.T) {
	var q startupQueue
	active := q.enqueue(true)
	warm := q.enqueue(false)
	first := q.enqueue(true)
	second := q.enqueue(true)
	assertReady := func(ticket *startupTicket, want bool) {
		t.Helper()
		select {
		case <-ticket.ready:
			if !want {
				t.Fatal("task admitted ahead of its turn")
			}
		default:
			if want {
				t.Fatal("next task was not admitted")
			}
		}
	}
	q.done(active)
	assertReady(first, true)
	assertReady(second, false)
	assertReady(warm, false)
	q.done(first)
	assertReady(second, true)
	q.done(second)
	assertReady(warm, true)
	q.done(warm)
}

func TestCancellingAQueuedStartupDoesNotReleaseTheActiveOne(t *testing.T) {
	var q startupQueue
	active := q.enqueue(true)
	cancelled := q.enqueue(true)
	next := q.enqueue(true)
	q.done(cancelled)
	select {
	case <-next.ready:
		t.Fatal("cancelling a waiter released the running operation")
	default:
	}
	q.done(active)
	select {
	case <-next.ready:
	default:
		t.Fatal("cancelled waiter blocked the next operation")
	}
	q.done(next)
}

func TestUnknownRunnerInventoryNeverRecreatesOrFailsTheRunner(t *testing.T) {
	a, tr, be, _ := newAgent(t, 2)
	be.listErr = backend.ErrUnavailable
	released := false
	task := createTask("uncertain", "existing")
	a.handleCreate(context.Background(), task, func() { released = true })
	if !released {
		t.Fatal("uncertain task retained its runner claim")
	}
	if created, _, removed := be.counts(); created != 0 || removed != 0 {
		t.Fatal("unknown inventory changed workloads")
	}
	select {
	case result := <-tr.results:
		t.Fatalf("uncertain existence was reported as a lifecycle outcome: %+v", result)
	default:
	}
	// The same task can recover on redelivery once inventory answers.
	be.listErr = nil
	a.handleCreate(context.Background(), task, func() {})
	if result := <-tr.results; !result.OK {
		t.Fatalf("redelivery did not recover: %+v", result)
	}
}

func TestRuntimeCooldownIsBoundedAndClearsOnSuccess(t *testing.T) {
	a, _, _, clock := newAgent(t, 2)
	a.runtimeResult(errors.New("bad pool configuration"))
	if !a.runtimeRetryAt.IsZero() {
		t.Fatal("configuration failure held the whole host")
	}
	for range 10 {
		a.runtimeResult(backend.ErrUnavailable)
		wait := a.runtimeRetryAt.Sub(clock.Now())
		if wait < 5*time.Second || wait > time.Minute {
			t.Fatalf("runtime cooldown outside bounds: %s", wait)
		}
	}
	a.runtimeResult(nil)
	if !a.runtimeRetryAt.IsZero() || a.runtimeFailures != 0 {
		t.Fatal("successful operation did not clear runtime hold")
	}
}

type dependencyBackend struct {
	*fakeBackend
	dindCalls int
	dindErr   error
}

func (b *dependencyBackend) PrewarmDinD(context.Context) error {
	b.dindCalls++
	return b.dindErr
}

func TestPrewarmingCoalescesSharedImagesAndIncludesDockerDependencies(t *testing.T) {
	a, _, be, clock := newAgent(t, 2)
	b := &dependencyBackend{fakeBackend: be}
	task := Task{PoolID: "one", Image: "runner", PullPolicy: store.PullAlways,
		Spec: &backend.Spec{DockerMode: store.DockerDinD}}
	for _, pool := range []string{"one", "two"} {
		task.PoolID = pool
		if _, err := a.prewarm(context.Background(), b, b, task); err != nil {
			t.Fatal(err)
		}
	}
	if len(be.pulls()) != 1 || b.dindCalls != 1 {
		t.Fatalf("shared preparation repeated: pulls=%v, dependencies=%d", be.pulls(), b.dindCalls)
	}
	clock.advance(time.Minute)
	if _, err := a.prewarm(context.Background(), b, b, task); err != nil {
		t.Fatal(err)
	}
	if len(be.pulls()) != 2 || b.dindCalls != 2 {
		t.Fatal("successful prewarm was reused past its freshness window")
	}
}

func TestFailedSidecarPreparationIsNotCached(t *testing.T) {
	a, _, be, _ := newAgent(t, 2)
	b := &dependencyBackend{fakeBackend: be, dindErr: errors.New("pull failed")}
	task := Task{Image: "runner", PullPolicy: store.PullIfNotPresent,
		Spec: &backend.Spec{DockerMode: store.DockerDinD}}
	if _, err := a.prewarm(context.Background(), b, b, task); err == nil {
		t.Fatal("sidecar preparation failure was ignored")
	}
	b.dindErr = nil
	if _, err := a.prewarm(context.Background(), b, b, task); err != nil {
		t.Fatal(err)
	}
	if b.dindCalls != 2 {
		t.Fatal("failed preparation suppressed its retry")
	}
}

type blockedStatsBackend struct {
	*fakeBackend
	entered chan struct{}
}

func (b *blockedStatsBackend) Stats(ctx context.Context, _ backend.Handle) (backend.Stats, error) {
	b.entered <- struct{}{}
	<-ctx.Done()
	return backend.Stats{}, ctx.Err()
}

func TestBlockedStatsDoNotBlockLifecycleReconciliation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, _, be, _ := newAgent(t, 2)
		b := &blockedStatsBackend{fakeBackend: be, entered: make(chan struct{}, 2)}
		a.opts.Backends = backend.NewRegistry(b)
		a.polled.Store(true)
		track(a, "runner-1", "wl-1", true).stats = backend.Stats{CPUPercent: 12.5}
		be.setWorkloads(running("wl-1", "runner-1"))
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan struct{})
		go func() {
			a.sampleStats(ctx)
			close(done)
		}()
		synctest.Wait()
		select {
		case <-b.entered:
		default:
			t.Fatal("sampler did not start")
		}
		reports, err := a.ReconcileOnce(context.Background())
		if err != nil || len(reports) != 1 || reports[0].Phase != backend.PhaseRunning {
			t.Fatalf("lifecycle observation was blocked: %+v, %v", reports, err)
		}
		cancel()
		synctest.Wait()
		<-done
		if got := a.Runners(); len(got) != 1 || got[0].Stats.CPUPercent != 12.5 {
			t.Fatalf("failed sampling replaced the last good reading: %+v", got)
		}
	})
}
